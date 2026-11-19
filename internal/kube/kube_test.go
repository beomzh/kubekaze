package kube

import "testing"

func TestIsProduction(t *testing.T) {
	yes := []string{"prod", "prod-eks", "eks-prod-1", "arn:aws:eks:ap-northeast-2:1:cluster/prod", "live", "my_production"}
	no := []string{"dev", "staging", "product-test", "reproduce", "improd"}
	for _, n := range yes {
		if !IsProduction(n) {
			t.Errorf("IsProduction(%q) = false, want true", n)
		}
	}
	for _, n := range no {
		if IsProduction(n) {
			t.Errorf("IsProduction(%q) = true, want false", n)
		}
	}
}

func TestIsProductionOverride(t *testing.T) {
	t.Setenv("KUBEKAZE_PROD_PATTERN", "^corp-")
	if !IsProduction("corp-main") || IsProduction("prod") {
		t.Error("override pattern not honoured")
	}
}
