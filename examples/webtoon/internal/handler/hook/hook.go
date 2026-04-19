package hook

// axe:wire:import

// RegisterAll subscribes all domain/external event handlers.
//
// This is the Hook Leader — it knows about event topics and nothing else.
// Decoupled from: Plugins, Routes, Services.
func RegisterAll( /* bus events.Bus */ ) {
	// axe:wire:hook
}
