package main

import (
	"archive/zip"
	"encoding/json"
	"fmt"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"gioui.org/app"
	"gioui.org/font"
	"gioui.org/font/gofont"
	"gioui.org/io/clipboard"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

// --- CONFIG ---
const (
	AppName       = "Ilöw Agent"
	RepoOwnerRepo = "Ilow-Space/IlowSniffer"
	GitHubAPIURL  = "https://api.github.com/repos/" + RepoOwnerRepo + "/releases/latest"

	// Release asset the installer replaces itself with; self-update is skipped if absent.
	InstallerAsset = "IlowInstaller.exe"
	BackgroundFlag = "--background"
	CheckInterval  = 6 * time.Hour
)

// GitHub API JSON Response Structs
type GHAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

type GHRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []GHAsset `json:"assets"`
}

// --- THEME COLORS ---
type ThemeColors struct {
	Bg          color.NRGBA
	Fg          color.NRGBA
	Accent      color.NRGBA
	Muted       color.NRGBA
	Faint       color.NRGBA
	SecondaryBg color.NRGBA
	DisabledBg  color.NRGBA
}

var theme = ThemeColors{
	Bg:          color.NRGBA{R: 10, G: 10, B: 10, A: 255},
	Fg:          color.NRGBA{R: 229, G: 231, B: 235, A: 255},
	Accent:      color.NRGBA{R: 59, G: 130, B: 246, A: 255},
	Muted:       color.NRGBA{R: 156, G: 163, B: 175, A: 255},
	Faint:       color.NRGBA{R: 107, G: 114, B: 128, A: 255},
	SecondaryBg: color.NRGBA{R: 51, G: 51, B: 51, A: 255},
	DisabledBg:  color.NRGBA{R: 50, G: 50, B: 50, A: 255},
}

// --- STATE ---
type AppState struct {
	Status        string
	LocalVersion  string
	RemoteVersion string
	DownloadURL   string
	InstallPath   string
	IsWorking     bool

	// Widgets
	BtnAction   widget.Clickable
	BtnCopyPath widget.Clickable
}

var state = AppState{
	Status:       "Initializing...",
	LocalVersion: "Unknown",
}

// --- MAIN ---
func main() {
	homeDir, _ := os.UserHomeDir()
	state.InstallPath = filepath.Join(homeDir, "AppData", "Local", "IlowAgent")
	if runtime.GOOS == "darwin" {
		state.InstallPath = filepath.Join(homeDir, "Library", "Application Support", "IlowAgent")
	}

	if exe, err := os.Executable(); err == nil {
		os.Remove(exe + ".old") // left behind by a previous self-update
	}
	enableAutostart() // re-written every launch so it follows the exe if it moves

	// Started at login: no window, just keep the extension and this exe current.
	if len(os.Args) > 1 && os.Args[1] == BackgroundFlag {
		for {
			backgroundCycle()
			time.Sleep(CheckInterval)
		}
	}

	go func() {
		w := new(app.Window)
		w.Option(
			app.Title("Ilöw Installer"),
			app.Size(unit.Dp(400), unit.Dp(600)),
			app.MinSize(unit.Dp(350), unit.Dp(500)),
		)

		if err := loop(w); err != nil {
			log.Fatal(err)
		}
		os.Exit(0)
	}()
	app.Main()
}

func loop(w *app.Window) error {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	th.Palette.Bg = theme.Bg
	th.Palette.Fg = theme.Fg
	th.Palette.ContrastBg = theme.Accent

	var ops op.Ops

	go checkVersion(w)

	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			if state.BtnAction.Clicked(gtx) && !state.IsWorking {
				go performUpdate(w)
			}
			if state.BtnCopyPath.Clicked(gtx) {
				gtx.Execute(clipboard.WriteCmd{
					Type: "application/text",
					Data: io.NopCloser(strings.NewReader(state.InstallPath)),
				})
				state.Status = "Path copied to clipboard!"
			}

			drawUI(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// --- UI LAYOUT ---
func drawUI(gtx layout.Context, th *material.Theme) layout.Dimensions {
	paintColor(gtx, th.Palette.Bg)
	inset := layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20), Right: unit.Dp(20)}

	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				h1 := material.H4(th, AppName)
				h1.Color = th.Palette.ContrastBg
				h1.Alignment = text.Middle
				return h1.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(30)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return drawInfoBox(gtx, th)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btnText := "CHECK FOR UPDATES"
				if state.IsWorking {
					btnText = "WORKING..."
				} else if state.LocalVersion != state.RemoteVersion && state.RemoteVersion != "" {
					btnText = "INSTALL UPDATE"
				}

				btn := material.Button(th, &state.BtnAction, btnText)
				if state.IsWorking {
					btn.Background = theme.DisabledBg
				}
				return btn.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(12), state.Status)
				lbl.Alignment = text.Middle
				lbl.Color = theme.Muted
				return lbl.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(30)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				div := material.Body2(th, "INSTRUCTIONS")
				div.Color = th.Palette.ContrastBg
				div.Font.Weight = font.Bold
				return div.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(material.Label(th, unit.Sp(13), "1. Click 'Install Update' above.").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(material.Label(th, unit.Sp(13), "2. Go to chrome://extensions").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(material.Label(th, unit.Sp(13), "3. Enable 'Developer Mode' (Top Right).").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(material.Label(th, unit.Sp(13), "4. Click 'Load Unpacked' and select:").Layout),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(th, unit.Sp(11), state.InstallPath)
						lbl.Color = theme.Faint
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, &state.BtnCopyPath, "COPY")
						btn.Background = theme.SecondaryBg
						btn.TextSize = unit.Sp(10)
						btn.Inset = layout.UniformInset(unit.Dp(5))
						return btn.Layout(gtx)
					}),
				)
			}),
		)
	})
}

func drawInfoBox(gtx layout.Context, th *material.Theme) layout.Dimensions {
	return layout.Flex{Axis: layout.Horizontal, Spacing: layout.SpaceBetween}.Layout(gtx,
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
				layout.Rigid(material.Label(th, unit.Sp(12), "INSTALLED VERSION").Layout),
				layout.Rigid(material.H6(th, state.LocalVersion).Layout),
			)
		}),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return layout.Flex{Axis: layout.Vertical, Alignment: layout.End}.Layout(gtx,
				layout.Rigid(material.Label(th, unit.Sp(12), "LATEST VERSION").Layout),
				layout.Rigid(material.H6(th, state.RemoteVersion).Layout),
			)
		}),
	)
}

func paintColor(gtx layout.Context, c color.NRGBA) {
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// --- LOGIC ---

func checkVersion(w *app.Window) {
	// 1. Read local version.txt created during extract or initial install
	localVerFile := filepath.Join(state.InstallPath, "version.txt")
	if data, err := os.ReadFile(localVerFile); err == nil {
		state.LocalVersion = normalizeVersion(string(data))
	} else {
		state.LocalVersion = "None"
	}

	// 2. Query GitHub REST API for the latest published Git release tag
	rel, err := fetchLatestGitRelease()
	if err == nil {
		state.RemoteVersion = normalizeVersion(rel.TagName)
		state.DownloadURL = getZipAssetURL(rel)

		if state.DownloadURL == "" {
			state.Status = "No .zip asset attached to latest Git release."
		} else if isNewerVersion(state.RemoteVersion, state.LocalVersion) {
			state.Status = "New version available!"
		} else {
			state.Status = "You are up to date."
		}
		selfUpdate(rel)
	} else {
		state.Status = "Error checking Git releases: " + err.Error()
	}

	w.Invalidate()
}

// Fetches latest published release object from GitHub repository
func fetchLatestGitRelease() (*GHRelease, error) {
	req, err := http.NewRequest("GET", GitHubAPIURL, nil)
	if err != nil {
		return nil, err
	}
	// GitHub API requires a User-Agent header
	req.Header.Set("User-Agent", "IlowAgent-Installer")

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("no releases found on GitHub repo yet")
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GitHub API status %d", resp.StatusCode)
	}

	var rel GHRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return nil, err
	}
	return &rel, nil
}

// Strips extra whitespace/newlines and optional 'v' prefix (e.g. "v2.1\n" -> "2.1")
func normalizeVersion(v string) string {
	v = strings.TrimSpace(v)
	v = strings.TrimPrefix(v, "v")
	return v
}

// Checks if remote tag is different / newer than local tag
func isNewerVersion(remote, local string) bool {
	if local == "None" || local == "" {
		return true
	}
	return remote != local
}

// Finds the .zip asset in the GitHub Release payload
func getZipAssetURL(rel *GHRelease) string {
	for _, asset := range rel.Assets {
		if strings.HasSuffix(asset.Name, ".zip") {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func getZipURL(rel *GHRelease) string {
	for _, asset := range rel.Assets {
		if strings.HasSuffix(asset.Name, ".zip") {
			return asset.BrowserDownloadURL
		}
	}
	return ""
}

func performUpdate(w *app.Window) {
	if state.DownloadURL == "" {
		state.Status = "Error: Download URL missing."
		w.Invalidate()
		return
	}

	state.IsWorking = true
	state.Status = "Downloading from GitHub..."
	w.Invalidate()

	time.Sleep(100 * time.Millisecond)

	err := installExtension()

	state.IsWorking = false
	if err != nil {
		state.Status = "Error: " + err.Error()
	} else {
		state.LocalVersion = state.RemoteVersion
		state.Status = "Success! Extension updated."
	}
	w.Invalidate()
}

func installExtension() error {
	os.MkdirAll(state.InstallPath, 0755)
	tmpZip := filepath.Join(os.TempDir(), "ilow_temp.zip")
	if err := downloadFile(tmpZip, state.DownloadURL); err != nil {
		return err
	}
	defer os.Remove(tmpZip)

	if err := unzip(tmpZip, state.InstallPath); err != nil {
		return err
	}
	// version.txt is written last: the extension reloads itself when it changes.
	return os.WriteFile(filepath.Join(state.InstallPath, "version.txt"), []byte(state.RemoteVersion), 0644)
}

// One headless check: update the extension files, then this exe.
func backgroundCycle() {
	rel, err := fetchLatestGitRelease()
	if err != nil {
		return
	}
	state.RemoteVersion = normalizeVersion(rel.TagName)
	state.DownloadURL = getZipAssetURL(rel)
	data, _ := os.ReadFile(filepath.Join(state.InstallPath, "version.txt"))
	if state.DownloadURL != "" && isNewerVersion(state.RemoteVersion, normalizeVersion(string(data))) {
		installExtension()
	}
	selfUpdate(rel)
}

// Swaps in the release's installer exe (a running exe can be renamed but not
// overwritten on Windows), relaunches with the same args and exits.
// ponytail: no checksum/signature, trusts the GitHub release over HTTPS; add if releases get signed.
func selfUpdate(rel *GHRelease) {
	if runtime.GOOS != "windows" {
		return
	}
	url := ""
	for _, a := range rel.Assets {
		if a.Name == InstallerAsset {
			url = a.BrowserDownloadURL
		}
	}
	tag := normalizeVersion(rel.TagName)
	marker := filepath.Join(state.InstallPath, "installer-version.txt")
	if data, _ := os.ReadFile(marker); url == "" || normalizeVersion(string(data)) == tag {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	if downloadFile(exe+".new", url) != nil {
		os.Remove(exe + ".new")
		return
	}
	os.Remove(exe + ".old")
	if os.Rename(exe, exe+".old") != nil {
		os.Remove(exe + ".new")
		return
	}
	if os.Rename(exe+".new", exe) != nil {
		os.Rename(exe+".old", exe)
		return
	}
	os.MkdirAll(state.InstallPath, 0755)
	os.WriteFile(marker, []byte(tag), 0644)
	exec.Command(exe, os.Args[1:]...).Start()
	os.Exit(0)
}

// --- FILE UTILS ---

func downloadFile(filepath string, url string) error {
	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "IlowAgent-Installer")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

func unzip(src string, dest string) error {
	r, err := zip.OpenReader(src)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		fpath := filepath.Join(dest, f.Name)
		if !strings.HasPrefix(fpath, filepath.Clean(dest)+string(os.PathSeparator)) {
			return fmt.Errorf("illegal file path")
		}
		if f.FileInfo().IsDir() {
			os.MkdirAll(fpath, os.ModePerm)
			continue
		}
		if err = os.MkdirAll(filepath.Dir(fpath), os.ModePerm); err != nil {
			return err
		}
		outFile, err := os.OpenFile(fpath, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, f.Mode())
		if err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		_, err = io.Copy(outFile, rc)
		outFile.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}
