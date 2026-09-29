package widgets

// The frame: options every card tile has besides its own config, stored
// in the same config map under frame_* keys (the type decoders ignore
// them):
//
//	┌─ accent ───────────────────────┐
//	│ [icon] Title (header, link)    │  header: normal | small | off
//	│ body … (density, money round)  │  refresh: auto | 60 … 3600 s
//	└────────────────────────────────┘  only_issues: hide while calm

import "strconv"

// HeaderMode is how a card shows its title.
type HeaderMode string

const (
	HeaderNormal HeaderMode = "normal"
	HeaderSmall  HeaderMode = "small"
	HeaderOff    HeaderMode = "off"
)

// RoundMode is how money reads on a tile.
type RoundMode string

const (
	RoundExact    RoundMode = "exact"
	RoundEuro     RoundMode = "euro"     // no cents
	RoundThousand RoundMode = "thousand" // 109 T€
)

// Frame is one tile's frame.
type Frame struct {
	Accent     string // "" = theme default
	Header     HeaderMode
	Link       string // the title opens this
	Icon       string // icon spec, as on link tiles
	RefreshS   int    // 0 = the type's own
	Dense      bool
	Round      RoundMode
	OnlyIssues bool // hide the tile while it reports nothing to do
}

// CalmSlot is the view key a type sets when there is nothing to do.
const CalmSlot = "Calm"

// calmTypes know when there is nothing to do (their view sets Calm).
var calmTypes = map[string]bool{}

// Calm marks a type as able to report "nothing to do".
func calmType(key string) { calmTypes[key] = true }

var (
	accentColors  = []string{"none", "yellow", "green", "red", "blue", "purple", "aqua", "orange"}
	headerModes   = []string{string(HeaderNormal), string(HeaderSmall), string(HeaderOff)}
	refreshChoice = []string{"auto", "60", "300", "900", "3600"}
	densityModes  = []string{"normal", "compact"}
	roundModes    = []string{string(RoundExact), string(RoundEuro), string(RoundThousand)}
)

// FrameFieldsOf returns the frame fields of a type; links draw no card.
func FrameFieldsOf(key string) []Field {
	if key == "link" {
		return nil
	}
	fields := []Field{
		sel("frame_accent", "none", accentColors...),
		sel("frame_header", string(HeaderNormal), headerModes...),
		{Key: "frame_link", Input: InputText},
		{Key: "frame_icon", Input: InputText},
		sel("frame_refresh", "auto", refreshChoice...),
		sel("frame_density", "normal", densityModes...),
		sel("frame_round", string(RoundExact), roundModes...),
	}
	if calmTypes[key] {
		fields = append(fields, Field{Key: "frame_only_issues", Input: InputCheck})
	}
	return fields
}

func oneOfStr(v any, allowed []string, def string) string {
	s := asString(v)
	for _, a := range allowed {
		if a == s {
			return s
		}
	}
	return def
}

// FrameOf reads a tile's frame from its raw config; unknown values fall
// back to the defaults.
func FrameOf(raw map[string]any) Frame {
	f := Frame{
		Header: HeaderMode(oneOfStr(raw["frame_header"], headerModes, string(HeaderNormal))),
		Link:   webURL(raw["frame_link"]),
		Icon:   asString(raw["frame_icon"]),
		Dense:  asString(raw["frame_density"]) == "compact",
		Round:  RoundMode(oneOfStr(raw["frame_round"], roundModes, string(RoundExact))),
	}
	if a := oneOfStr(raw["frame_accent"], accentColors, "none"); a != "none" {
		f.Accent = a
	}
	if r := oneOfStr(raw["frame_refresh"], refreshChoice, "auto"); r != "auto" {
		f.RefreshS, _ = strconv.Atoi(r)
	}
	f.OnlyIssues, _ = raw["frame_only_issues"].(bool)
	return f
}
