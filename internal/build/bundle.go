package build

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/evanw/esbuild/pkg/api"
)

// BundleWorker compiles a Workers-style entry module into a single ES module.
//
// The bundle is produced with the same resolution rules `wrangler deploy` uses,
// so imports of npm packages and relative files keep working unchanged. Doing
// it here rather than on the server is the whole point of the design: the
// artifact that leaves this machine is already the exact module workerd loads.
func BundleWorker(entry, outFile string, minify bool) error {
	result := api.Build(api.BuildOptions{
		EntryPoints:       []string{entry},
		Outfile:           outFile,
		Bundle:            true,
		Write:             true,
		Format:            api.FormatESModule,
		Platform:          api.PlatformNeutral,
		Target:            api.ES2022,
		MainFields:        []string{"module", "main"},
		Conditions:        []string{"workerd", "worker", "browser", "import"},
		MinifyWhitespace:  minify,
		MinifyIdentifiers: minify,
		MinifySyntax:      minify,
		LegalComments:     api.LegalCommentsNone,
		Sourcemap:         api.SourceMapNone,
		LogLevel:          api.LogLevelSilent,
	})
	if len(result.Errors) > 0 {
		return fmt.Errorf("could not bundle %s:\n%s", filepath.Base(entry), formatMessages(result.Errors))
	}
	return nil
}

// formatMessages turns esbuild diagnostics into the file:line:col form editors
// and terminals already know how to make clickable.
func formatMessages(msgs []api.Message) string {
	var b strings.Builder
	for i, m := range msgs {
		if i >= 10 {
			fmt.Fprintf(&b, "  | ... and %d more\n", len(msgs)-i)
			break
		}
		if m.Location != nil {
			fmt.Fprintf(&b, "  | %s:%d:%d: %s\n", m.Location.File, m.Location.Line, m.Location.Column, m.Text)
			if strings.TrimSpace(m.Location.LineText) != "" {
				fmt.Fprintf(&b, "  |   %s\n", strings.TrimSpace(m.Location.LineText))
			}
			continue
		}
		fmt.Fprintf(&b, "  | %s\n", m.Text)
	}
	return strings.TrimRight(b.String(), "\n")
}
