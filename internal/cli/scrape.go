package cli

import (
	"flag"
	"fmt"
	"strings"

	"github.com/abdulovia/cookcompass/internal/recipes"
)

type multiFlag []string

func (m *multiFlag) String() string     { return strings.Join(*m, ",") }
func (m *multiFlag) Set(v string) error { *m = append(*m, v); return nil }

func runScrape(e *env, args []string) error {
	fs := flag.NewFlagSet("scrape", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	source := fs.String("source", "jsonld", "recipe source name")
	limit := fs.Int("limit", 0, "stop after this many recipes (0 = all)")
	var urls multiFlag
	fs.Var(&urls, "url", "scrape this URL instead of the targets file (repeatable)")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	res, err := a.ScrapeRecipes(e.ctx, *source, recipes.FetchOpts{URLs: urls, Limit: *limit})
	fmt.Fprintln(e.Stdout, res.String())
	for _, ie := range res.Errors {
		fmt.Fprintf(e.Stderr, "  skipped: %v\n", ie)
	}
	if err != nil {
		if res.Fetched == 0 && !strings.Contains(err.Error(), "unknown source") {
			// Offline or every target blocked: not fatal, the seed data still works.
			e.log.Warn("nothing could be fetched; keeping existing data", "err", err)
			return nil
		}
		return err
	}
	if res.Created+res.Updated > 0 {
		fmt.Fprintln(e.Stdout, "review new items at /admin/queue or run `make approve-all`")
	}
	return nil
}
