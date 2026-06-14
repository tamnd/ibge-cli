package ibge

import (
	"context"
	"fmt"
	"strings"
	"unicode"

	"github.com/tamnd/any-cli/kit"
	"github.com/tamnd/any-cli/kit/errs"
)

// domain.go exposes ibge as a kit Domain: a driver that a multi-domain
// host (ant) enables with a single blank import,
//
//	import _ "github.com/tamnd/ibge-cli/ibge"
//
// exactly as a database/sql program enables a driver with `import _
// "github.com/lib/pq"`. The init below registers it; the host then dereferences
// ibge:// URIs by routing to the operations Register installs. The same
// Domain also builds the standalone ibge binary (see cli.NewApp), so the
// binary and a host share one source of truth.
func init() { kit.Register(Domain{}) }

// Domain is the ibge driver. It carries no state; the per-run client is
// built by the factory Register hands kit.
type Domain struct{}

// Info describes the scheme, the hostnames a pasted link is matched against, and
// the identity reused for the binary's help and version.
func (Domain) Info() kit.DomainInfo {
	return kit.DomainInfo{
		Scheme: "ibge",
		Hosts:  []string{Host},
		Identity: kit.Identity{
			Binary: "ibge",
			Short:  "A command line for the IBGE public API.",
			Long: `A command line for the IBGE (Brazilian Institute of Geography and Statistics)
public API at servicodados.ibge.gov.br.

ibge reads states, regions, municipalities, name-frequency data, and news
over plain HTTPS, shapes it into clean records, and prints output that pipes
into the rest of your tools. No API key, nothing to run alongside it.`,
			Site: "www.ibge.gov.br",
			Repo: "https://github.com/tamnd/ibge-cli",
		},
	}
}

// Register installs the client factory and every operation onto app.
func (Domain) Register(app *kit.App) {
	app.SetClient(newClient)

	kit.Handle(app, kit.OpMeta{
		Name:    "states",
		Group:   "geography",
		List:    true,
		Summary: "List all 27 Brazilian states",
		URIType: "state",
	}, listStates)

	kit.Handle(app, kit.OpMeta{
		Name:    "regions",
		Group:   "geography",
		List:    true,
		Summary: "List the 5 Brazilian macro-regions",
		URIType: "region",
	}, listRegions)

	kit.Handle(app, kit.OpMeta{
		Name:    "municipalities",
		Group:   "geography",
		List:    true,
		Summary: "List Brazilian municipalities, optionally filtered by state",
		URIType: "municipality",
	}, listMunicipalities)

	kit.Handle(app, kit.OpMeta{
		Name:    "names",
		Group:   "census",
		List:    true,
		Summary: "Show census frequency data for a name across Brazil",
		URIType: "name",
		Args:    []kit.Arg{{Name: "name", Help: "name to search frequency for"}},
	}, listNames)

	kit.Handle(app, kit.OpMeta{
		Name:    "news",
		Group:   "news",
		List:    true,
		Summary: "List recent IBGE news",
		URIType: "news",
	}, listNews)
}

// newClient builds the client from the host-resolved config.
func newClient(_ context.Context, cfg kit.Config) (any, error) {
	c := NewClient()
	if cfg.UserAgent != "" {
		c.UserAgent = cfg.UserAgent
	}
	if cfg.Rate > 0 {
		c.Rate = cfg.Rate
	}
	if cfg.Retries > 0 {
		c.Retries = cfg.Retries
	}
	if cfg.Timeout > 0 {
		c.HTTP.Timeout = cfg.Timeout
	}
	return c, nil
}

// ---------------------------------------------------------------------------
// Input structs
// ---------------------------------------------------------------------------

type statesInput struct {
	Client *Client `kit:"inject"`
}

type regionsInput struct {
	Client *Client `kit:"inject"`
}

type municipalitiesInput struct {
	State  string  `kit:"flag" help:"state code e.g. SP"`
	Limit  int     `kit:"flag,inherit" help:"max results" default:"20"`
	Client *Client `kit:"inject"`
}

type namesInput struct {
	Name   string  `kit:"arg" help:"name to search frequency for"`
	Client *Client `kit:"inject"`
}

type newsInput struct {
	Limit  int     `kit:"flag,inherit" help:"max news items" default:"5"`
	Client *Client `kit:"inject"`
}

// ---------------------------------------------------------------------------
// Handlers
// ---------------------------------------------------------------------------

func listStates(ctx context.Context, in statesInput, emit func(*State) error) error {
	states, err := in.Client.GetStates(ctx)
	if err != nil {
		return mapErr(err)
	}
	for _, s := range states {
		if err := emit(s); err != nil {
			return err
		}
	}
	return nil
}

func listRegions(ctx context.Context, in regionsInput, emit func(*Region) error) error {
	regions, err := in.Client.GetRegions(ctx)
	if err != nil {
		return mapErr(err)
	}
	for _, r := range regions {
		if err := emit(r); err != nil {
			return err
		}
	}
	return nil
}

func listMunicipalities(ctx context.Context, in municipalitiesInput, emit func(*Municipality) error) error {
	munis, err := in.Client.GetMunicipalities(ctx, in.State, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, m := range munis {
		if err := emit(m); err != nil {
			return err
		}
	}
	return nil
}

func listNames(ctx context.Context, in namesInput, emit func(*NameFrequency) error) error {
	freqs, err := in.Client.GetNameFrequency(ctx, in.Name)
	if err != nil {
		return mapErr(err)
	}
	for _, f := range freqs {
		if err := emit(f); err != nil {
			return err
		}
	}
	return nil
}

func listNews(ctx context.Context, in newsInput, emit func(*News) error) error {
	items, err := in.Client.GetNews(ctx, in.Limit)
	if err != nil {
		return mapErr(err)
	}
	for _, n := range items {
		if err := emit(n); err != nil {
			return err
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// Resolver: pure string functions, network-free
// ---------------------------------------------------------------------------

// Classify turns any accepted input into the canonical (type, id).
// - numeric string → ("municipality", id)
// - 2-letter uppercase → ("state", code)
// - otherwise → ("name", name)
func (Domain) Classify(input string) (uriType, id string, err error) {
	input = strings.TrimSpace(input)
	if input == "" {
		return "", "", errs.Usage("empty ibge reference")
	}
	if isNumeric(input) {
		return "municipality", input, nil
	}
	if len(input) == 2 && isUpperAlpha(input) {
		return "state", input, nil
	}
	return "name", input, nil
}

// Locate is the inverse: the live https URL for a (type, id).
func (Domain) Locate(uriType, id string) (string, error) {
	switch uriType {
	case "state":
		return fmt.Sprintf("https://www.ibge.gov.br/cidades-e-estados/%s.html", strings.ToLower(id)), nil
	case "municipality":
		return "https://www.ibge.gov.br/cidades-e-estados", nil
	default:
		return "https://www.ibge.gov.br", nil
	}
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

func isNumeric(s string) bool {
	for _, r := range s {
		if !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func isUpperAlpha(s string) bool {
	for _, r := range s {
		if !unicode.IsUpper(r) || !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

func mapErr(err error) error {
	return err
}
