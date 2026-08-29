package types

import "testing"

func TestModelPricingRejectsNegativeAndUnsupportedCurrency(t *testing.T) {
	negative := ModelPricing{Currency: "USD", InputPerMillion: -1}
	if err := negative.NormalizeAndValidate(); err == nil {
		t.Fatal("negative price was accepted")
	}
	otherCurrency := ModelPricing{Currency: "CNY", InputPerMillion: 1}
	if err := otherCurrency.NormalizeAndValidate(); err == nil {
		t.Fatal("unsupported currency was accepted")
	}
	valid := ModelPricing{Currency: " usd ", CacheReadPerMillion: 0.1}
	if err := valid.NormalizeAndValidate(); err != nil || valid.Currency != "USD" {
		t.Fatalf("valid pricing was not normalized: %#v err=%v", valid, err)
	}
}
