package variants

import (
	"fmt"
	"strings"

	. "github.com/zond/goaeoas"
	"github.com/zond/godip/variants"
)

func handleRenderMap(w ResponseWriter, r Request) error {
	phase := &Phase{
		Variant: r.Vars()["name"],
	}
	if err := phase.FromQuery(r.Req().URL.Query()); err != nil {
		return err
	}

	return RenderPhaseMap(w, r, phase)
}

func RenderPhaseMap(w ResponseWriter, r Request, phase *Phase) error {
	variant := variants.Variants[phase.Variant]

	mapURL, err := router.Get(VariantMapRoute).URL("name", phase.Variant)
	if err != nil {
		return err
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")

	staticJSBuf := []string{}
	for _, nat := range variant.Nations {
		staticJSBuf = append(staticJSBuf, fmt.Sprintf("var col%s;", nat))
	}

	jsBuf := []string{}
	for i, nat := range variant.Nations {
		jsBuf = append(jsBuf, fmt.Sprintf("col%s = map.contrasts[%d];", nat, i))
	}
	for prov, unit := range phase.Units {
		jsBuf = append(jsBuf, fmt.Sprintf("map.addUnit('unit%s', %q, col%s);", unit.Type, prov, unit.Nation))
	}
	for prov, unit := range phase.Dislodgeds {
		jsBuf = append(jsBuf, fmt.Sprintf("map.addUnit('unit%s', %q, col%s, true);", unit.Type, prov, unit.Nation))
	}
	for _, prov := range variant.Graph.Provinces() {
		if prov.Super() == prov {
			if variant.Graph.SC(prov) != nil {
				if nat, found := phase.SupplyCenters[prov]; found {
					jsBuf = append(jsBuf, fmt.Sprintf("map.colorProvince(%q, col%s);", prov, nat))
				} else {
					jsBuf = append(jsBuf, fmt.Sprintf("map.hideProvince(%q);", prov))
				}
			} else {
				jsBuf = append(jsBuf, fmt.Sprintf("map.hideProvince(%q);", prov))
			}
		}
	}
	jsBuf = append(jsBuf, "map.showProvinces();")
	for nat, orders := range phase.Orders {
		for prov, order := range orders {
			parts := []string{fmt.Sprintf("%q", prov)}
			for _, part := range order {
				parts = append(parts, fmt.Sprintf("%q", part))
			}
			jsBuf = append(jsBuf, fmt.Sprintf("map.addOrder([%s], col%s);", strings.Join(parts, ","), nat))
		}
	}

	htmlNode := NewEl("html")
	headNode := htmlNode.AddEl("head")
	headNode.AddEl("title").AddText(fmt.Sprintf("%s %v, %s", phase.Season, phase.Year, phase.Type))
	headNode.AddEl("script", "src", "https://ajax.googleapis.com/ajax/libs/jquery/3.1.1/jquery.min.js")
	headNode.AddEl("script", "src", "/js/dippymap.js")
	headNode.AddEl("script").AddText(fmt.Sprintf(
		`
%s
window.map = function() {
  var that = this;
	that.readyActions = [];
  that.addReadyAction = function(cb) {
		that.readyActions.push(cb);
	};
	return that;
}();
$(document).ready(function() {
	$.ajax({
		url: %q,
		dataType: 'html',
		success: function(data) {
			$('#map').append(data);
			var readyActions = window.map.readyActions;
      window.map = dippyMap($('#map'));
%s
			for (var i = 0; i < readyActions.length; i++) {
				readyActions[i]();
			}
		}
	});
});
`, strings.Join(staticJSBuf, "\n"), mapURL.String(), strings.Join(jsBuf, "\n")))

	bodyNode := htmlNode.AddEl("body", "style", "background:#212121;")
	bodyNode.AddEl("div", "id", "map")
	for _, typ := range variant.UnitTypes {
		hiddenRoot := bodyNode.AddEl("div", "id", fmt.Sprintf("unit%s", typ), "style", "display:none;")
		unitBytes, err := variant.SVGUnits[typ]()
		if err != nil {
			return err
		}
		if _, err := hiddenRoot.AddRaw(unitBytes); err != nil {
			return err
		}
	}

	return htmlNode.Render(w)
}
