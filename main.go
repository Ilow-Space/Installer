package main

import (
	"archive/zip"
	"fmt"
	"image/color"
	"io"
	"log"
	"net/http"
	"os"
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
	AppName          = "Ilöw Sniffer"
	RemoteVersionURL = "https://ilow.io/static/dist/sniffer/version.txt"   // CHANGE THIS
	RemoteZipURL     = "https://ilow.io/static/dist/sniffer/extension.zip" // CHANGE THIS
)

// --- STATE ---
type AppState struct {
	Status        string
	LocalVersion  string
	RemoteVersion string
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
	// Determine install path on launch
	homeDir, _ := os.UserHomeDir()
	state.InstallPath = filepath.Join(homeDir, "AppData", "Local", "IlowAgent")
	if runtime.GOOS == "darwin" {
		state.InstallPath = filepath.Join(homeDir, "Library", "Application Support", "IlowAgent")
	}

	go func() {
		// NEW: Window creation syntax for latest Gio
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
	// NEW: Font handling
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))

	// Custom Colors (Dark Theme)
	th.Palette.Bg = color.NRGBA{R: 20, G: 20, B: 20, A: 255}           // #141414
	th.Palette.Fg = color.NRGBA{R: 230, G: 230, B: 230, A: 255}        // White text
	th.Palette.ContrastBg = color.NRGBA{R: 16, G: 185, B: 129, A: 255} // #10b981 (Green)

	var ops op.Ops

	// Check version on startup in background
	go checkVersion(w)

	for {
		e := w.Event()
		switch e := e.(type) {
		case app.DestroyEvent:
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)

			// Process Button Clicks
			if state.BtnAction.Clicked(gtx) && !state.IsWorking {
				go performUpdate(w)
			}
			if state.BtnCopyPath.Clicked(gtx) {
				// NEW: Clipboard syntax
				gtx.Execute(clipboard.WriteCmd{
					Type: "application/text",
					Data: io.NopCloser(strings.NewReader(state.InstallPath)),
				})
				state.Status = "Path copied to clipboard!"
			}

			// Draw Interface
			drawUI(gtx, th)
			e.Frame(gtx.Ops)
		}
	}
}

// --- UI LAYOUT ---
func drawUI(gtx layout.Context, th *material.Theme) layout.Dimensions {
	// Background fill
	paintColor(gtx, th.Palette.Bg)

	// Padding
	inset := layout.Inset{Top: unit.Dp(20), Bottom: unit.Dp(20), Left: unit.Dp(20), Right: unit.Dp(20)}

	return inset.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
			// 1. Header
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				h1 := material.H4(th, AppName)
				h1.Color = th.Palette.ContrastBg
				h1.Alignment = text.Middle
				return h1.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(30)}.Layout),

			// 2. Version Info Box
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return drawInfoBox(gtx, th)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(20)}.Layout),

			// 3. Main Action Button
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				btnText := "CHECK FOR UPDATES"
				if state.IsWorking {
					btnText = "WORKING..."
				} else if state.LocalVersion != state.RemoteVersion && state.RemoteVersion != "" {
					btnText = "INSTALL UPDATE"
				}

				btn := material.Button(th, &state.BtnAction, btnText)
				if state.IsWorking {
					btn.Background = color.NRGBA{R: 50, G: 50, B: 50, A: 255}
				}
				return btn.Layout(gtx)
			}),

			// 4. Status Text
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				lbl := material.Label(th, unit.Sp(12), state.Status)
				lbl.Alignment = text.Middle
				lbl.Color = color.NRGBA{R: 150, G: 150, B: 150, A: 255}
				return lbl.Layout(gtx)
			}),

			layout.Rigid(layout.Spacer{Height: unit.Dp(30)}.Layout),

			// 5. Instructions Divider
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				div := material.Body2(th, "INSTRUCTIONS")
				div.Color = th.Palette.ContrastBg
				div.Font.Weight = font.Bold // NEW: Use font package
				return div.Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),

			// 6. Instruction Steps
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Label(th, unit.Sp(13), "1. Click 'Install Update' above.").Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Label(th, unit.Sp(13), "2. Go to chrome://extensions").Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Label(th, unit.Sp(13), "3. Enable 'Developer Mode' (Top Right).").Layout(gtx)
			}),
			layout.Rigid(layout.Spacer{Height: unit.Dp(5)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return material.Label(th, unit.Sp(13), "4. Click 'Load Unpacked' and select:").Layout(gtx)
			}),

			// 7. Path Display & Copy Button
			layout.Rigid(layout.Spacer{Height: unit.Dp(10)}.Layout),
			layout.Rigid(func(gtx layout.Context) layout.Dimensions {
				return layout.Flex{Axis: layout.Horizontal, Alignment: layout.Middle}.Layout(gtx,
					layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
						lbl := material.Label(th, unit.Sp(11), state.InstallPath)
						lbl.Color = color.NRGBA{R: 100, G: 100, B: 100, A: 255} // Dim
						return lbl.Layout(gtx)
					}),
					layout.Rigid(layout.Spacer{Width: unit.Dp(10)}.Layout),
					layout.Rigid(func(gtx layout.Context) layout.Dimensions {
						btn := material.Button(th, &state.BtnCopyPath, "COPY")
						btn.Background = color.NRGBA{R: 60, G: 60, B: 60, A: 255}
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
	// NEW: Rect variable used properly implicitly by PaintOp? No, we need explicit clip.
	// Actually, just painting color over Max constraints is enough for background.
	paint.ColorOp{Color: c}.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
}

// --- LOGIC ---

func checkVersion(w *app.Window) {
	// 1. Check Local
	localVerFile := filepath.Join(state.InstallPath, "version.txt")
	if data, err := os.ReadFile(localVerFile); err == nil {
		state.LocalVersion = strings.TrimSpace(string(data))
	} else {
		state.LocalVersion = "None"
	}

	// 2. Check Remote
	if remote, err := fetchString(RemoteVersionURL); err == nil {
		state.RemoteVersion = strings.TrimSpace(remote)
		state.Status = "Ready to update."
		if state.LocalVersion == state.RemoteVersion {
			state.Status = "You are up to date."
		}
	} else {
		state.Status = "Error connecting to server."
	}

	w.Invalidate()
}

func performUpdate(w *app.Window) {
	state.IsWorking = true
	state.Status = "Downloading..."
	w.Invalidate()

	// Wait a moment so UI updates
	time.Sleep(100 * time.Millisecond)

	err := func() error {
		// Create Dir
		os.MkdirAll(state.InstallPath, 0755)

		// Download Zip
		tmpZip := filepath.Join(os.TempDir(), "ilow_temp.zip")
		if err := downloadFile(tmpZip, RemoteZipURL); err != nil {
			return err
		}
		defer os.Remove(tmpZip)

		// Unzip
		state.Status = "Extracting files..."
		w.Invalidate()
		if err := unzip(tmpZip, state.InstallPath); err != nil {
			return err
		}

		// Update Version File
		localVerFile := filepath.Join(state.InstallPath, "version.txt")
		os.WriteFile(localVerFile, []byte(state.RemoteVersion), 0644)

		return nil
	}()

	state.IsWorking = false
	if err != nil {
		state.Status = "Error: " + err.Error()
	} else {
		state.LocalVersion = state.RemoteVersion
		state.Status = "Success! Extension updated."
	}
	w.Invalidate()
}

// --- FILE UTILS ---
func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}

func fetchString(url string) (string, error) {
	resp, err := http.Get(url)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return string(body), err
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
