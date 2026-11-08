package theme

import (
	"strings"
	"testing"
)

func TestNewResolvesDefault(t *testing.T) {
	for _, name := range []string{"", "default", "DEFAULT", "unknown"} {
		th := New(name)
		if th.Name != DefaultName {
			t.Errorf("New(%q).Name = %q, want %q", name, th.Name, DefaultName)
		}
	}
}

func TestStylesRender(t *testing.T) {
	th := Default()
	styles := map[string]struct {
		style interface{ Render(...string) string }
	}{
		"header":        {th.Header},
		"sidebar":       {th.Sidebar},
		"main":          {th.Main},
		"tab active":    {th.TabActive},
		"tab inactive":  {th.TabInactive},
		"status":        {th.StatusBar},
		"status key":    {th.StatusKey},
		"modal":         {th.Modal},
		"modal title":   {th.ModalTitle},
		"error":         {th.Error},
		"dim":           {th.Dim},
		"null":          {th.Null},
		"notice":        {th.Notice},
		"section title": {th.SectionTitle},
	}

	for name, item := range styles {
		if rendered := item.style.Render("x"); !strings.Contains(rendered, "x") {
			t.Errorf("%s style dropped its content: %q", name, rendered)
		}
	}
}
