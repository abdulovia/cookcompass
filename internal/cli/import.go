package cli

import (
	"flag"
	"fmt"

	"github.com/abdulovia/cookcompass/internal/app"
)

func runImportRecipes(e *env, args []string) error {
	fs := flag.NewFlagSet("recipes-import", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	file := fs.String("file", "", "dataset file inside the recipe inbox (data/recipe-inbox by default)")
	url := fs.String("url", "", "http(s) or file: URL of a dataset (≤5 MB for http)")
	limit := fs.Int("limit", 0, "stop after this many recipes (0 = all)")
	approve := fs.Bool("approve", false, "approve what this run creates (earlier pending items stay queued)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *file == "" && *url == "" {
		return fmt.Errorf("either --file or --url is required")
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	res, err := a.ImportDataset(e.ctx, app.DatasetOptions{File: *file, URL: *url, Limit: *limit, Approve: *approve})
	fmt.Fprintln(e.Stdout, res.String())
	for _, ie := range res.Errors {
		fmt.Fprintf(e.Stderr, "  skipped: %v\n", ie)
	}
	if err != nil && res.Fetched == 0 {
		e.log.Warn("nothing could be read from the dataset", "err", err)
		return nil
	}
	if res.Created+res.Updated > 0 && !*approve {
		fmt.Fprintln(e.Stdout, "review new items at /admin/queue or run `make approve-all`")
	}
	return nil
}
