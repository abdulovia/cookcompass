package cli

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"text/tabwriter"

	"github.com/abdulovia/cookcompass/internal/recipes"
)

// cliChange is recorded on revisions made from the command line.
var cliChange = recipes.Change{Source: recipes.SourceUser, Note: "cli"}

func runRecipe(e *env, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: recipe create|delete|restore|list [flags]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("recipe "+sub, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	switch sub {
	case "create":
		file := fs.String("file", "", "recipe JSON file (\"-\" for stdin); see docs/examples/recipe.json")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return recipeCreate(e, *file)
	case "delete", "restore":
		id := fs.Int64("id", 0, "recipe id")
		slug := fs.String("slug", "", "recipe slug (alternative to --id)")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return recipeLifecycle(e, sub, *id, *slug)
	case "list":
		deleted := fs.Bool("deleted", false, "list deleted recipes")
		status := fs.String("status", recipes.StatusApproved, "approved|pending|rejected")
		if err := fs.Parse(rest); err != nil {
			return err
		}
		return recipeList(e, *status, *deleted)
	}
	return fmt.Errorf("unknown recipe subcommand %q", sub)
}

func recipeCreate(e *env, file string) error {
	if file == "" {
		return errors.New("--file is required")
	}
	var r io.Reader = e.Stdin
	if file != "-" {
		f, err := os.Open(file)
		if err != nil {
			return err
		}
		defer f.Close()
		r = f
	}
	var in recipes.Input
	dec := json.NewDecoder(r)
	dec.DisallowUnknownFields()
	if err := dec.Decode(&in); err != nil {
		return fmt.Errorf("parse %s: %w", file, err)
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	rec, err := a.Recipes.FromInput(e.ctx, in)
	if err != nil {
		return err
	}
	rec.Source = recipes.SourceUser
	if err := a.Recipes.Create(e.ctx, rec, cliChange); err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "created recipe %d (%s)\n", rec.ID, rec.Slug)
	return nil
}

func recipeLifecycle(e *env, action string, id int64, slug string) error {
	if id == 0 && slug == "" {
		return errors.New("--id or --slug is required")
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	if id == 0 {
		r, err := a.Recipes.GetBySlug(e.ctx, slug)
		if err != nil {
			return err
		}
		id = r.ID
	}
	fn := a.Recipes.SoftDelete
	if action == "restore" {
		fn = a.Recipes.Restore
	}
	if err := fn(e.ctx, id, cliChange); err != nil {
		if errors.Is(err, recipes.ErrNotFound) {
			return fmt.Errorf("recipe %d not found or already %sd", id, action)
		}
		return err
	}
	fmt.Fprintf(e.Stdout, "recipe %d %sd\n", id, action)
	return nil
}

func recipeList(e *env, status string, deleted bool) error {
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	f := recipes.Filter{Statuses: []string{status}}
	if deleted {
		f.Deleted = recipes.DeletedOnly
	}
	list, err := a.Recipes.List(e.ctx, f)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(e.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "ID\tSLUG\tDIFFICULTY\tSOURCE\tNAME")
	for _, r := range list {
		fmt.Fprintf(tw, "%d\t%s\t%s\t%s\t%s\n", r.ID, r.Slug, r.Difficulty, r.Source, r.Name)
	}
	return tw.Flush()
}

func runApproveAll(e *env, _ []string) error {
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	nr, err := a.Recipes.ApproveAllPending(e.ctx, recipes.Change{Source: recipes.SourceUser, Note: "approve-all"})
	if err != nil {
		return err
	}
	np, err := a.Catalog.ApproveAllPending(e.ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "approved %d recipes and %d products\n", nr, np)
	return nil
}

func runPurgePending(e *env, _ []string) error {
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	nr, err := a.Recipes.PurgePending(e.ctx)
	if err != nil {
		return err
	}
	np, err := a.Catalog.PurgePending(e.ctx)
	if err != nil {
		return err
	}
	fmt.Fprintf(e.Stdout, "purged %d pending recipes and %d pending products\n", nr, np)
	return nil
}
