package minutes

import (
	"encoding/json"
	"testing"

	"github.com/monalbarse/kahan-hai/backend/internal/adapter"
)

func TestPaise(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want int
	}{
		{"whole rupees with zero paise", "36.00", 3600},
		{"no decimal point at all", "40", 4000},
		{"thousands separator", "1,299.5", 129950},
		{"single fraction digit is tenths", "0.5", 50},
		{"three fraction digits truncate", "36.456", 3645},
		{"surrounding whitespace", "  72.00  ", 7200},
		{"trailing point", "12.", 1200},
		{"empty", "", 0},
		{"not a number", "free", 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := paise(c.in); got != c.want {
				t.Errorf("paise(%q) = %d, want %d", c.in, got, c.want)
			}
		})
	}
}

func TestMRPPaise(t *testing.T) {
	t.Run("prefers the dedicated mrp field", func(t *testing.T) {
		var v productValue
		v.Pricing.MRP = &price{DecimalValue: "99.00"}
		v.Pricing.Prices = []price{{PriceType: "MRP", DecimalValue: "50.00"}}
		if got := mrpPaise(v); got != 9900 {
			t.Errorf("got %d, want 9900", got)
		}
	})

	// Promoted listings null out mrp even when discounted, and carry the
	// struck-off price in the prices array instead.
	t.Run("falls back to the prices array when mrp is null", func(t *testing.T) {
		var v productValue
		v.Pricing.Prices = []price{
			{PriceType: "FSP", DecimalValue: "72.00"},
			{PriceType: "MRP", DecimalValue: "83.00"},
		}
		if got := mrpPaise(v); got != 8300 {
			t.Errorf("got %d, want 8300", got)
		}
	})

	t.Run("no mrp anywhere", func(t *testing.T) {
		if got := mrpPaise(productValue{}); got != 0 {
			t.Errorf("got %d, want 0", got)
		}
	})
}

func TestImageURL(t *testing.T) {
	var v productValue
	if got := imageURL(v); got != "" {
		t.Errorf("no images should give empty string, got %q", got)
	}

	v.Media.Images = []struct {
		URL string `json:"url"`
	}{{URL: "https://rukmini.com/image/{@width}/{@height}/x/q/{@quality}/p.jpeg"}}

	want := "https://rukmini.com/image/416/416/x/q/70/p.jpeg"
	if got := imageURL(v); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

// romeFixture is the shape of one page/fetch response, trimmed to the fields
// the adapter reads. The real payload is several hundred kilobytes.
const romeFixture = `{
  "RESPONSE": {
    "slots": [
      {"widget": {"type": "SOME_BANNER", "data": {"products": [
        {"productInfo": {"value": {"id": "IGNORED", "titles": {"title": "Banner"}}}}
      ]}}},
      {"widget": {"type": "PRODUCT_SUMMARY_EXTENDED", "data": {"products": [
        {"productInfo": {
          "action": {"url": "/p/itm123", "params": {"shopId": ["STORE9"]}},
          "value": {
            "id": "P1",
            "productBrand": "Amul",
            "titles": {"title": "Amul Gold Milk", "subtitle": "1 L"},
            "availability": {"displayState": "IN_STOCK"},
            "pricing": {"finalPrice": {"decimalValue": "72.00"},
                        "mrp": {"decimalValue": "83.00"}}
          }
        }},
        {"productInfo": {
          "action": {"url": "/p/itm456"},
          "value": {
            "id": "P2",
            "titles": {"title": "Amul Taaza", "subtitle": "500 ml"},
            "availability": {"displayState": "OUT_OF_STOCK"},
            "smartUrl": "https://www.flipkart.com/smart/456",
            "pricing": {"finalPrice": {"decimalValue": "29.00"}}
          }
        }}
      ]}}}
    ]
  }
}`

func TestParse(t *testing.T) {
	a := &Adapter{}
	got := a.parse([][]byte{[]byte(romeFixture)})

	if len(got) != 2 {
		t.Fatalf("parsed %d products, want 2 (the banner widget must be skipped)", len(got))
	}

	first := got[0]
	want := adapter.Product{
		Platform:   adapter.Minutes,
		ID:         "P1",
		Name:       "Amul Gold Milk",
		Brand:      "Amul",
		Variant:    "1 L",
		PricePaise: 7200,
		MRPPaise:   8300,
		InStock:    true,
		DeepLink:   origin + "/p/itm123",
		StoreID:    "STORE9",
	}
	if first != want {
		t.Errorf("first product:\n got %+v\nwant %+v", first, want)
	}
	if first.DiscountPercent() != 13 {
		t.Errorf("discount = %d, want 13", first.DiscountPercent())
	}

	second := got[1]
	if second.InStock {
		t.Error("P2 is OUT_OF_STOCK and should not be marked in stock")
	}
	// smartUrl wins over the relative action URL when the platform supplies it.
	if second.DeepLink != "https://www.flipkart.com/smart/456" {
		t.Errorf("deep link = %q, want the smartUrl", second.DeepLink)
	}
}

func TestParseSkipsUnusableEntriesAndDuplicates(t *testing.T) {
	a := &Adapter{}

	// The same payload twice: rome repeats products across paging responses,
	// and the parser is handed every captured body each time it runs.
	got := a.parse([][]byte{[]byte(romeFixture), []byte(romeFixture)})
	if len(got) != 2 {
		t.Errorf("duplicate payloads produced %d products, want 2", len(got))
	}

	if n := len(a.parse([][]byte{[]byte("not json at all")})); n != 0 {
		t.Errorf("unparseable body produced %d products, want 0", n)
	}

	var blank struct{}
	b, _ := json.Marshal(blank)
	if n := len(a.parse([][]byte{b})); n != 0 {
		t.Errorf("empty object produced %d products, want 0", n)
	}
}
