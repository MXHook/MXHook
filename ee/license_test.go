package ee_test

import (
	"testing"

	"github.com/mxhook/mxhook/ee"
)

func TestLicense_IsValidWithKey(t *testing.T) {
	l := ee.ParseLicense("valid-ee-key-abc123")
	if !l.IsValid() {
		t.Error("expected valid license")
	}
}

func TestLicense_IsInvalidWithoutKey(t *testing.T) {
	l := ee.ParseLicense("")
	if l.IsValid() {
		t.Error("expected invalid license")
	}
}

func TestLicense_Require_PanicsWithoutKey(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on Require() without valid license")
		}
	}()
	l := ee.ParseLicense("")
	l.Require("analytics")
}

func TestLicense_Require_DoesNotPanicWithKey(t *testing.T) {
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("unexpected panic: %v", r)
		}
	}()
	l := ee.ParseLicense("valid-key")
	l.Require("analytics")
}

func TestLicense_Gate_ReturnsFalseWithoutKey(t *testing.T) {
	l := ee.ParseLicense("")
	if l.Gate("rbac") {
		t.Error("expected Gate to return false without license")
	}
}

func TestLicense_Gate_ReturnsTrueWithKey(t *testing.T) {
	l := ee.ParseLicense("valid-key")
	if !l.Gate("rbac") {
		t.Error("expected Gate to return true with license")
	}
}
