package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/dikdotcom/stackly/internal/fingerprint"
	"github.com/dikdotcom/stackly/internal/scanner"
)

func main() {
	var (
		fpPath    = flag.String("fingerprints", "data/fingerprints.json", "Path to fingerprints JSON")
		url       = flag.String("url", "", "Target URL to scan")
		jsonOut   = flag.Bool("json", false, "Output as JSON")
		timeout   = flag.Duration("timeout", 30*time.Second, "Scan timeout")
		showData  = flag.Bool("data", false, "Show collected page data")
		listTechs = flag.Bool("list", false, "List all known technologies")
		listCats  = flag.Bool("categories", false, "List categories")
	)
	flag.Parse()

	// Load fingerprints
	db, err := fingerprint.Load(*fpPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error loading fingerprints: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "✓ Loaded %d technologies from %s\n", len(db.Technologies), *fpPath)

	// List mode
	if *listTechs {
		fmt.Println("Known technologies:")
		for _, tech := range db.Technologies {
			fmt.Printf("  [%s] %s — %s\n", tech.Category, tech.Slug, tech.Name)
		}
		return
	}

	// Categories mode
	if *listCats {
		cats := make(map[string]int)
		for _, tech := range db.Technologies {
			cats[tech.Category]++
		}
		fmt.Println("Categories:")
		for cat, count := range cats {
			fmt.Printf("  %s: %d\n", cat, count)
		}
		return
	}

	if *url == "" {
		flag.Usage()
		os.Exit(1)
	}

	// Create scanner
	s := scanner.NewScanner(db)
	s.SetTimeout(*timeout)

	fmt.Fprintf(os.Stderr, "→ Scanning %s ...\n", *url)
	result := s.ScanURL(getContext(), "", *url)

	if result.Error != nil {
		fmt.Fprintf(os.Stderr, "✗ Error: %v\n", result.Error)
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "✓ Done in %s\n", result.ScanDuration)

	// Output
	if *jsonOut {
		outputJSON(result)
	} else {
		outputText(result, *showData)
	}
}

func outputText(r *scanner.ScanResult, showData bool) {
	fmt.Println()
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Printf(" Scan Results: %s\n", r.URL)
	fmt.Printf(" Duration: %s\n", r.ScanDuration.Round(time.Millisecond))
	fmt.Println("═══════════════════════════════════════════════════════════════")
	fmt.Println()

	if len(r.Results) == 0 {
		fmt.Println("No technologies detected.")
		return
	}

	// Group by category
	cats := make(map[string][]scanner.Result)
	for _, res := range r.Results {
		cats[res.Technology.Category] = append(cats[res.Technology.Category], res)
	}

	for cat, results := range cats {
		fmt.Printf("┌── %s\n", cat)
		for _, res := range results {
			fmt.Printf("│  • %s", res.Technology.Name)
			if res.Confidence > 100 {
				fmt.Printf(" (confidence: %d)", res.Confidence)
			}
			fmt.Println()
		}
		fmt.Println("│")
	}

	// Implied technologies
	implied := scanner.GetImplied(r.Results)
	if len(implied) > 0 {
		fmt.Println("┌── Implied")
		for _, slug := range implied {
			fmt.Printf("│  → %s\n", slug)
		}
		fmt.Println("│")
	}

	fmt.Println("═══════════════════════════════════════════════════════════════")

	if showData {
		fmt.Println()
		fmt.Println("Collected page data:")
		fmt.Printf("  HTML length:    %d bytes\n", len(r.HTML))
		fmt.Printf("  Scripts:        %d\n", len(r.Scripts))
		fmt.Printf("  CSS links:      %d\n", len(r.CSSLinks))
		fmt.Printf("  Meta tags:      %d\n", len(r.MetaTags))
		fmt.Printf("  JS globals:     %d (%v)\n", len(r.JSGlobals), r.JSGlobals)
		fmt.Printf("  Cookies:        %d\n", len(r.Cookies))
		fmt.Printf("  Headers:        %d\n", len(r.Headers))

		fmt.Println("\n  Response headers:")
		for k, v := range r.Headers {
			fmt.Printf("    %s: %s\n", k, v[0])
		}
	}
}

func outputJSON(r *scanner.ScanResult) {
	type jsonResult struct {
		Technology string   `json:"technology"`
		Slug       string   `json:"slug"`
		Category   string   `json:"category"`
		Confidence int      `json:"confidence"`
		Matches    []string `json:"matches"`
	}

	type jsonOut struct {
		URL      string       `json:"url"`
		Duration string       `json:"duration"`
		Detected []jsonResult `json:"detected"`
		Implied  []string     `json:"implied"`
	}

	out := jsonOut{
		URL:      r.URL,
		Duration: r.ScanDuration.String(),
	}

	for _, res := range r.Results {
		var matchStrs []string
		for _, m := range res.Matches {
			matchStrs = append(matchStrs, fmt.Sprintf("[%s] %s", m.Type, m.Rule))
		}
		out.Detected = append(out.Detected, jsonResult{
			Technology: res.Technology.Name,
			Slug:       res.Technology.Slug,
			Category:   res.Technology.Category,
			Confidence: res.Confidence,
			Matches:    matchStrs,
		})
	}

	out.Implied = scanner.GetImplied(r.Results)

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}

func getContext() context.Context {
	return context.Background()
}