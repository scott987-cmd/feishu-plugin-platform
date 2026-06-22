// Package dsl defines the "application definition" — the declarative JSON that
// is the platform's hard currency. One AppDefinition drives both the in-Feishu
// container renderer and the opdev export compiler.
package dsl

import (
	"errors"
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// AppDefinition is a single generated app/plugin, stored as data and rendered
// at runtime by the container plugin.
type AppDefinition struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Type    string   `json:"type"` // view_extension | automation
	Bind    Bind     `json:"bind"`
	UI      UI       `json:"ui"`
	Actions []Action `json:"actions,omitempty"`
	Version int      `json:"version,omitempty"`
}

// Bind ties the app to a host Bitable. "current" means the base the plugin is
// opened in (resolved client-side via js-sdk).
type Bind struct {
	BaseID  string `json:"baseId"`
	TableID string `json:"tableId"`
}

// UI is the declarative component tree the renderer walks.
type UI struct {
	Layout     string      `json:"layout"` // dashboard | list | form
	Components []Component `json:"components"`
}

// Component is one renderable unit. Fields are a superset; only those relevant
// to the component Type are used.
type Component struct {
	Type      string   `json:"type"` // stat | chart | table | text
	Title     string   `json:"title,omitempty"`
	Agg       string   `json:"agg,omitempty"` // sum | count | avg | max | min
	Field     string   `json:"field,omitempty"`
	Filter    string   `json:"filter,omitempty"`
	ChartType string   `json:"chartType,omitempty"` // bar | line | pie
	X         string   `json:"x,omitempty"`
	Y         *AggSpec `json:"y,omitempty"`
	Text      string   `json:"text,omitempty"`
	Columns   []string `json:"columns,omitempty"` // table only: column names to show (omit = all)
	Target    float64  `json:"target,omitempty"`  // gauge only: target value; progress = value / target
	Col       string   `json:"col,omitempty"`     // pivot only: column group-by field (row uses x)
	Sort      string   `json:"sort,omitempty"`    // chart only: asc | desc (TopN with limit)
	Limit     int      `json:"limit,omitempty"`   // chart only: take top N groups
}

// AggSpec is an aggregation over a field, used e.g. as a chart's Y axis.
type AggSpec struct {
	Agg   string `json:"agg"`
	Field string `json:"field"`
}

// Action is a declarative behavior (no arbitrary code) the renderer can wire to
// a trigger. The long tail that does not fit declarative actions is handled by
// the sandbox (Phase 4), not here.
type Action struct {
	ID      string `json:"id"`
	Trigger string `json:"trigger"` // button | onLoad
	Label   string `json:"label,omitempty"`
	Do      string `json:"do"` // exportXlsx | notify | ...
	Scope   string `json:"scope,omitempty"`
}

// Allowed enum values. Keeping these explicit is what lets us validate
// LLM/template output and refuse anything off-schema before it is stored.
var (
	ValidTypes      = []string{"view_extension", "automation"}
	ValidComponents = []string{"stat", "chart", "table", "text", "gauge", "pivot", "timeline", "kanban", "countdown", "markdown", "gallery", "calendar"}
	ValidAggs       = []string{"sum", "count", "avg", "max", "min", "median", "distinct", "range", "stddev"}
	ValidCharts     = []string{"bar", "line", "pie"}
	ValidActions    = []string{"exportXlsx", "notify"}
	ValidTriggers   = []string{"button", "onLoad"}
	ValidSorts      = []string{"asc", "desc"}
)

// Size bounds. The 1MB body limit upstream is not enough on its own — a 1MB
// payload can still encode tens of thousands of components, which would be
// re-serialized to every renderer. Bound the shape here too.
const (
	MaxComponents = 100
	MaxActions    = 50
	MaxNameLen    = 256
	MaxStrLen     = 512
	MaxColumns    = 50 // table component column count cap
)

// idRe constrains ID because it is used as a URL path segment in
// GET/DELETE /api/apps/{id}.
var idRe = regexp.MustCompile(`^[A-Za-z0-9._-]{1,128}$`)

// Validate returns a joined error describing every schema violation, or nil.
// This is the gate every generated definition must pass before being stored.
func (d AppDefinition) Validate() error {
	var errs []error
	if strings.TrimSpace(d.ID) == "" {
		errs = append(errs, errors.New("id is required"))
	} else if !idRe.MatchString(d.ID) {
		errs = append(errs, fmt.Errorf("id %q invalid (must match %s)", d.ID, idRe.String()))
	}
	if strings.TrimSpace(d.Name) == "" {
		errs = append(errs, errors.New("name is required"))
	} else if len(d.Name) > MaxNameLen {
		errs = append(errs, fmt.Errorf("name too long (>%d bytes)", MaxNameLen))
	}
	if !slices.Contains(ValidTypes, d.Type) {
		errs = append(errs, fmt.Errorf("type %q invalid (want one of: %s)", d.Type, strings.Join(ValidTypes, ", ")))
	}
	if len(d.UI.Components) == 0 {
		errs = append(errs, errors.New("ui.components must not be empty"))
	}
	if len(d.UI.Components) > MaxComponents {
		errs = append(errs, fmt.Errorf("ui.components too many (%d > %d)", len(d.UI.Components), MaxComponents))
	}
	for i, c := range d.UI.Components {
		if !slices.Contains(ValidComponents, c.Type) {
			errs = append(errs, fmt.Errorf("components[%d].type %q invalid (want one of: %s)", i, c.Type, strings.Join(ValidComponents, ", ")))
		}
		if c.Agg != "" && !slices.Contains(ValidAggs, c.Agg) {
			errs = append(errs, fmt.Errorf("components[%d].agg %q invalid (want one of: %s)", i, c.Agg, strings.Join(ValidAggs, ", ")))
		}
		if c.Type == "chart" {
			if c.ChartType == "" {
				errs = append(errs, fmt.Errorf("components[%d] is a chart but chartType is empty (want one of: %s)", i, strings.Join(ValidCharts, ", ")))
			} else if !slices.Contains(ValidCharts, c.ChartType) {
				errs = append(errs, fmt.Errorf("components[%d].chartType %q invalid (want one of: %s)", i, c.ChartType, strings.Join(ValidCharts, ", ")))
			}
		}
		if c.Y != nil && c.Y.Agg != "" && !slices.Contains(ValidAggs, c.Y.Agg) {
			errs = append(errs, fmt.Errorf("components[%d].y.agg %q invalid (want one of: %s)", i, c.Y.Agg, strings.Join(ValidAggs, ", ")))
		}
		// Free-form strings are length-bounded. Note: Filter is a formula consumed
		// later by the Bitable query/export engine — it MUST be parsed/allowlisted
		// at that point, never string-interpolated.
		for _, f := range []struct{ name, val string }{
			{"title", c.Title}, {"field", c.Field}, {"filter", c.Filter}, {"x", c.X}, {"text", c.Text}, {"col", c.Col},
		} {
			if len(f.val) > MaxStrLen {
				errs = append(errs, fmt.Errorf("components[%d].%s too long (>%d bytes)", i, f.name, MaxStrLen))
			}
		}
		if len(c.Columns) > MaxColumns {
			errs = append(errs, fmt.Errorf("components[%d].columns too many (%d > %d)", i, len(c.Columns), MaxColumns))
		}
		for j, col := range c.Columns {
			if len(col) > MaxStrLen {
				errs = append(errs, fmt.Errorf("components[%d].columns[%d] too long (>%d bytes)", i, j, MaxStrLen))
			}
		}
		if c.Sort != "" && !slices.Contains(ValidSorts, c.Sort) {
			errs = append(errs, fmt.Errorf("components[%d].sort %q invalid (want one of: %s)", i, c.Sort, strings.Join(ValidSorts, ", ")))
		}
	}
	if len(d.Actions) > MaxActions {
		errs = append(errs, fmt.Errorf("actions too many (%d > %d)", len(d.Actions), MaxActions))
	}
	for i, a := range d.Actions {
		if strings.TrimSpace(a.ID) == "" {
			errs = append(errs, fmt.Errorf("actions[%d].id is required", i))
		}
		if !slices.Contains(ValidTriggers, a.Trigger) {
			errs = append(errs, fmt.Errorf("actions[%d].trigger %q invalid (want one of: %s)", i, a.Trigger, strings.Join(ValidTriggers, ", ")))
		}
		if a.Do == "" {
			errs = append(errs, fmt.Errorf("actions[%d].do is required", i))
		} else if !slices.Contains(ValidActions, a.Do) {
			errs = append(errs, fmt.Errorf("actions[%d].do %q invalid (want one of: %s)", i, a.Do, strings.Join(ValidActions, ", ")))
		}
	}
	return errors.Join(errs...)
}
