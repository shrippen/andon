package widgetlib_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"andon/internal/enums"
	"andon/internal/i18n"
	"andon/internal/model"
	"andon/internal/services/widgetlib"
	"andon/internal/testkit"
	"andon/internal/widgets"
)

// TestEveryDetailFromDemo: each type with a detail dialog builds it from
// its gallery demo data without a panic, and every block carries the data
// its kind draws. A new type is covered without being listed anywhere.
func TestEveryDetailFromDemo(t *testing.T) {
	d := testkit.DB(t)
	who, space := testkit.User(t, d, "demo@x.y", enums.RoleAdmin)

	for _, kind := range widgets.AllTypes() {
		if kind.Detail == nil {
			continue
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		w := &model.Widget{SpaceID: space, Type: kind.Key, Title: kind.Key}
		dialog, err := widgetlib.DemoDetail(ctx, d, who, w)
		cancel()
		if err != nil {
			t.Errorf("%s: %v", kind.Key, err)
			continue
		}
		body, ok := dialog.Body.(*widgets.DetailBody)
		if !ok {
			continue // a template of its own
		}
		for _, err := range checkBody(body) {
			t.Errorf("%s: %v", kind.Key, err)
		}
		for _, key := range textKeys(body, dialog.Head) {
			if !i18n.Has(key) {
				t.Errorf("%s: no catalog text %q", kind.Key, key)
			}
		}
	}
}

// checkBody lists blocks whose data does not fit their kind.
func checkBody(b *widgets.DetailBody) []error {
	var errs []error
	var walk func(blocks []widgets.Block)
	walk = func(blocks []widgets.Block) {
		for i, bl := range blocks {
			ok := true
			switch bl.Kind {
			case widgets.BlockGraph:
				_, ok = bl.Data.(widgets.Graph)
			case widgets.BlockTable:
				_, ok = bl.Data.(widgets.Table)
			case widgets.BlockStrips:
				_, ok = bl.Data.([]widgets.Strip)
			case widgets.BlockBars:
				_, ok = bl.Data.([]widgets.ShareBar)
			case widgets.BlockTimeline:
				_, ok = bl.Data.([]widgets.Event)
			case widgets.BlockRows, widgets.BlockStatus:
				_, ok = bl.Data.([]widgets.LitRow)
			case widgets.BlockHints:
				_, ok = bl.Data.([]widgets.DetailHint)
			case widgets.BlockWall:
				_, ok = bl.Data.([]widgets.Card)
			case widgets.BlockTasks:
				_, ok = bl.Data.(widgets.Tasks)
			case widgets.BlockHeat:
				_, ok = bl.Data.(widgets.Heat)
			case widgets.BlockDay:
				_, ok = bl.Data.(widgets.DayCard)
			case widgets.BlockChips:
				_, ok = bl.Data.([]string)
			case widgets.BlockCode:
				_, ok = bl.Data.(string)
			case widgets.BlockWeek:
				_, ok = bl.Data.(widgets.Week)
			case widgets.BlockDayStrip:
				_, ok = bl.Data.(widgets.DayStrip)
			case widgets.BlockImage:
				_, ok = bl.Data.(widgets.Image)
			case widgets.BlockRead:
				_, ok = bl.Data.(widgets.Reading)
			case widgets.BlockForm:
				_, ok = bl.Data.(widgets.Form)
			case widgets.BlockThumbs:
				_, ok = bl.Data.([]widgets.Image)
			case widgets.BlockMap:
				_, ok = bl.Data.(*widgets.MapData)
			case widgets.BlockFrame:
				_, ok = bl.Data.(widgets.Embed)
			case widgets.BlockText:
			case widgets.BlockPair:
				pair, isPair := bl.Data.([]widgets.Block)
				ok = isPair
				walk(pair)
			default:
				ok = false
			}
			if !ok {
				errs = append(errs, fmt.Errorf("block %d (%s): data %T", i, bl.Kind, bl.Data))
			}

			// Meta is drawn as a value: a Text label there shows raw.
			if _, isText := bl.Meta.(widgets.Text); isText {
				errs = append(errs, fmt.Errorf("block %d (%s): Meta is a Text, want a value (Txt)", i, bl.Kind))
			}
		}
	}
	walk(b.Blocks)
	for _, tab := range b.Tabs {
		walk(tab.Blocks)
	}
	return errs
}

// textKeys lists the catalog keys a dialog shows.
func textKeys(b *widgets.DetailBody, h widgets.DetailHead) []string {
	keys := []string{h.StateKey}
	for _, a := range h.Actions {
		keys = append(keys, a.LabelKey)
	}
	add := func(t widgets.Text) { keys = append(keys, t.Key) }
	for _, f := range append(append(append([]widgets.Fact{}, b.Line...), b.Side...), b.End...) {
		add(f.Label)
	}
	for _, k := range b.Facts {
		add(k.Label)
	}
	if b.List != nil {
		add(b.List.Label)
		add(b.List.StateText)
	}
	var walk func(blocks []widgets.Block)
	walk = func(blocks []widgets.Block) {
		for _, bl := range blocks {
			add(bl.Label)
			switch v := bl.Data.(type) {
			case widgets.Table:
				for _, h := range v.Head {
					add(h)
				}
			case []widgets.Card:
				for _, c := range v {
					add(c.Label)
				}
			case widgets.Tasks:
				add(v.Label)
				for _, it := range v.Items {
					add(it.Action)
				}
			case widgets.DayCard:
				add(v.StateText)
				for _, k := range v.Kpis {
					add(k.Label)
				}
			case []widgets.Block:
				walk(v)
			}
		}
	}
	walk(b.Blocks)
	for _, tab := range b.Tabs {
		add(tab.Label)
		walk(tab.Blocks)
	}
	out := keys[:0]
	for _, k := range keys {
		if k != "" {
			out = append(out, k)
		}
	}
	return out
}
