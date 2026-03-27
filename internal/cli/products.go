package cli

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"

	"github.com/abdulovia/cookcompass/internal/catalog"
)

func runScan(e *env, args []string) error {
	fs := flag.NewFlagSet("scan", flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	country := fs.String("country", "all", "country code or \"all\"")
	if err := fs.Parse(args); err != nil {
		return err
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	results, err := a.ScanCatalog(e.ctx, *country)
	if err != nil {
		return err
	}
	for _, r := range results {
		fmt.Fprintln(e.Stdout, r.String())
		for _, ie := range r.Errors {
			fmt.Fprintf(e.Stderr, "  %s: %v\n", r.Country, ie)
		}
	}
	fmt.Fprintln(e.Stdout, "new items wait in /admin/queue (or run `make approve-all`)")
	return nil
}

func runProducts(e *env, args []string) error {
	if len(args) == 0 {
		return errors.New("usage: products import|export [flags]")
	}
	sub, rest := args[0], args[1:]
	fs := flag.NewFlagSet("products "+sub, flag.ContinueOnError)
	fs.SetOutput(e.Stderr)
	country := fs.String("country", "", "country code (required)")
	file := fs.String("file", "", "CSV or JSON file (import)")
	out := fs.String("out", "", "write CSV here instead of stdout (export)")
	if err := fs.Parse(rest); err != nil {
		return err
	}
	if *country == "" || strings.EqualFold(*country, "all") {
		return errors.New("--country is required (e.g. --country US)")
	}
	a, err := e.open()
	if err != nil {
		return err
	}
	defer a.Close()
	if _, err := a.Catalog.Country(e.ctx, *country); err != nil {
		return err
	}
	switch sub {
	case "import":
		if *file == "" {
			return errors.New("--file is required")
		}
		res, err := a.ImportProductsFile(e.ctx, *file, *country)
		if err != nil {
			return err
		}
		fmt.Fprintf(e.Stdout, "created %d, updated %d, failed %d\n", res.Created, res.Updated, len(res.Errors))
		for _, ie := range res.Errors {
			fmt.Fprintf(e.Stderr, "  %v\n", ie)
		}
		if len(res.Errors) > 0 {
			return fmt.Errorf("%d rows failed", len(res.Errors))
		}
		return nil
	case "export":
		list, err := a.Catalog.ListProducts(e.ctx, catalog.ProductFilter{Country: *country})
		if err != nil {
			return err
		}
		w := e.Stdout
		if *out != "" {
			f, err := os.Create(*out)
			if err != nil {
				return err
			}
			defer f.Close()
			w = f
		}
		return catalog.WriteCSV(w, list)
	}
	return fmt.Errorf("unknown products subcommand %q", sub)
}
