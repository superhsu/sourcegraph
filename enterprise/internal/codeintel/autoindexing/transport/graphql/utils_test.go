package graphql

import (
	"testing"
)

func TestCursor(t *testing.T) {
	expected := "test"
	pageInfo := EncodeCursor(&expected)

	if !pageInfo.HasNextPage() {
		t.Fatalf("expected next page")
	}
	if pageInfo.EndCursor() == nil {
		t.Fatalf("unexpected nil cursor")
	}

	value, err := DecodeCursor(pageInfo.EndCursor())
	if err != nil {
		t.Fatalf("unexpected error decoding cursor: %s", err)
	}
	if value != expected {
		t.Errorf("unexpected decoded cursor. want=%s have=%s", expected, value)
	}
}

func TestCursorEmpty(t *testing.T) {
	pageInfo := EncodeCursor(nil)

	if pageInfo.HasNextPage() {
		t.Errorf("unexpected next page")
	}
	if pageInfo.EndCursor() != nil {
		t.Errorf("unexpected encoded cursor: %s", *pageInfo.EndCursor())
	}

	value, err := DecodeCursor(nil)
	if err != nil {
		t.Fatalf("unexpected error decoding cursor: %s", err)
	}
	if value != "" {
		t.Errorf("unexpected decoded cursor: %s", value)
	}
}

func TestIntCursor(t *testing.T) {
	expected := 42
	pageInfo := EncodeIntCursor(toInt32(&expected))

	if !pageInfo.HasNextPage() {
		t.Fatalf("expected next page")
	}
	if pageInfo.EndCursor() == nil {
		t.Fatalf("unexpected nil cursor")
	}

	value, err := DecodeIntCursor(pageInfo.EndCursor())
	if err != nil {
		t.Fatalf("unexpected error decoding cursor: %s", err)
	}
	if value != expected {
		t.Errorf("unexpected decoded cursor. want=%d have=%d", expected, value)
	}
}

func TestIntCursorEmpty(t *testing.T) {
	pageInfo := EncodeIntCursor(nil)

	if pageInfo.HasNextPage() {
		t.Errorf("unexpected next page")
	}
	if pageInfo.EndCursor() != nil {
		t.Errorf("unexpected encoded cursor: %s", *pageInfo.EndCursor())
	}

	value, err := DecodeIntCursor(nil)
	if err != nil {
		t.Fatalf("unexpected error decoding cursor: %s", err)
	}
	if value != 0 {
		t.Errorf("unexpected decoded cursor: %d", value)
	}
}

func TestIndexID(t *testing.T) {
	expected := int64(42)
	value, err := unmarshalLSIFIndexGQLID(marshalLSIFIndexGQLID(expected))
	if err != nil {
		t.Fatalf("unexpected error marshalling id: %s", err)
	}
	if value != expected {
		t.Errorf("unexpected id. have=%d want=%d", expected, value)
	}
}

func TestDerefInt32(t *testing.T) {
	expected := 42
	expected32 := int32(expected)

	if val := derefInt32(nil, expected); val != expected {
		t.Errorf("unexpected value. want=%d have=%d", expected, val)
	}
	if val := derefInt32(&expected32, expected); val != expected {
		t.Errorf("unexpected value. want=%d have=%d", expected, val)
	}
}

func TestDerefString(t *testing.T) {
	expected := "foo"

	if val := derefString(nil, expected); val != expected {
		t.Errorf("unexpected value. want=%s have=%s", expected, val)
	}
	if val := derefString(&expected, ""); val != expected {
		t.Errorf("unexpected value. want=%s have=%s", expected, val)
	}
	if val := derefString(&expected, expected); val != expected {
		t.Errorf("unexpected value. want=%s have=%s", expected, val)
	}
}
