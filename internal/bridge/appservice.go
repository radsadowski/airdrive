package bridge

import (
	"fmt"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// AppService is the shell-level Wails service: app metadata, not storage API.
type AppService struct {
	app *application.App
}

func NewAppService(app *application.App) *AppService {
	return &AppService{app: app}
}

// SetApp injects the app reference after application.New returns.
func (s *AppService) SetApp(app *application.App) { s.app = app }

// Version returns the backend version.
func (s *AppService) Version() string { return "0.1.0" }

// Platform returns the OS the backend is running on.
func (s *AppService) Platform() string { return "darwin" }

// PickFolder opens the native macOS folder picker and returns the chosen path,
// or an empty string if the user canceled.
func (s *AppService) PickFolder() (string, error) {
	fmt.Println("PickFolder called")
	path, err := s.app.Dialog.OpenFile().
		SetTitle("Choose a folder").
		CanChooseDirectories(true).
		CanChooseFiles(false).
		PromptForSingleSelection()
	if err != nil {
		return "", err
	}
	return path, nil
}
