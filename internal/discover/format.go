package discover

import (
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strings"
	"text/tabwriter"
)

// Output is the machine-readable envelope printed in -json mode.
type Output struct {
	Count   int      `json:"count"`
	Proxies []Result `json:"proxies"`
}

// WriteResults renders discovered proxies. With asJSON it writes a JSON
// envelope to stdout; otherwise it writes an aligned table. When no proxies
// were found it says so: to stdout in table mode, to stderr in JSON mode so
// stdout stays a valid JSON document.
func WriteResults(stdout, stderr io.Writer, results []Result, asJSON bool) error {
	if asJSON {
		if err := WriteJSON(stdout, results); err != nil {
			return err
		}
		if len(results) == 0 {
			fmt.Fprintln(stderr, "no proxies found")
		}
		return nil
	}

	if len(results) == 0 {
		fmt.Fprintln(stdout, "no proxies found")
		return nil
	}
	return WriteTable(stdout, results)
}

// WriteJSON writes the results as a JSON envelope {"count":N,"proxies":[...]}.
func WriteJSON(w io.Writer, results []Result) error {
	out := Output{Count: len(results), Proxies: results}
	if out.Proxies == nil {
		out.Proxies = []Result{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return fmt.Errorf("discover: encode json: %w", err)
	}
	return nil
}

// WriteTable writes the results as an aligned table with one row per proxy.
func WriteTable(w io.Writer, results []Result) error {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	if _, err := fmt.Fprintln(tw, "INSTANCE\tHOST\tADDRESS\tPORT\tTXT"); err != nil {
		return err
	}
	for _, r := range results {
		if _, err := fmt.Fprintf(tw, "%s\t%s\t%s\t%d\t%s\n",
			r.Instance, r.Host, formatAddresses(r.Addresses), r.Port, formatTXT(r.TXT)); err != nil {
			return err
		}
	}
	return tw.Flush()
}

func formatAddresses(addresses []string) string {
	if len(addresses) == 0 {
		return "-"
	}
	return strings.Join(addresses, ",")
}

// formatTXT renders TXT entries as sorted "key=value" pairs; a key without a
// value renders bare, and an empty map renders as "-".
func formatTXT(txt map[string]string) string {
	if len(txt) == 0 {
		return "-"
	}
	keys := make([]string, 0, len(txt))
	for k := range txt {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	parts := make([]string, 0, len(keys))
	for _, k := range keys {
		if v := txt[k]; v != "" {
			parts = append(parts, k+"="+v)
			continue
		}
		parts = append(parts, k)
	}
	return strings.Join(parts, " ")
}
