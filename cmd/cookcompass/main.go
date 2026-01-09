// Command cookcompass runs the CookCompass web app and its maintenance tools.
package main

import (
	"os"

	"github.com/abdulovia/cookcompass/internal/cli"
)

func main() {
	app := &cli.App{Stdout: os.Stdout, Stderr: os.Stderr, Stdin: os.Stdin}
	os.Exit(app.Run(os.Args[1:]))
}
