package main

import "context"

// App is the Wails binding layer.
type App struct {
	ctx context.Context
}

// NewApp builds the App.
func NewApp() *App {
	return &App{}
}

// startup stores the Wails context.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
}

// Greet is a temporary placeholder verifying the binding pipeline.
func (a *App) Greet(name string) string {
	return "Hello " + name
}
