package setup

import (
	"context"

	"github.com/axe-cute/examples-webtoon/config"
	"github.com/axe-cute/axe/pkg/plugin"
	// axe:wire:import
)

// RegisterPlugins loads all configured plugins into the app.
//
// This is the Plugin Leader — it knows how to initialize plugins and nothing else.
// Decoupled from: Routes, Hooks, Services.
func RegisterPlugins(_ context.Context, app *plugin.App, cfg *config.Config) error {
	// axe:wire:plugin
	return nil
}
