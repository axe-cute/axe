package handler

// Controllers holds all HTTP handlers for the application.
// `axe generate resource` adds new fields here via the axe:wire:controller marker.
type Controllers struct {
	Auth *AuthHandler
	User *UserHandler
	Docs *OpenAPIHandler
	// axe:wire:controller
}
