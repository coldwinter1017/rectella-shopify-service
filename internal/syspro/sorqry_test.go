package syspro

import (
	"context"
	"encoding/xml"
	"strings"
	"testing"
)

func TestBuildSORQRY(t *testing.T) {
	xmlStr, err := buildSORQRY("SO12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(xmlStr, "<SalesOrderNumber>SO12345</SalesOrderNumber>") {
		t.Errorf("expected SalesOrderNumber in XML, got: %s", xmlStr)
	}
	for _, tag := range []string{
		"<IncludeStockedLines>N</IncludeStockedLines>",
		"<IncludeNonStockedLines>N</IncludeNonStockedLines>",
		"<IncludeFreightLines>N</IncludeFreightLines>",
		"<IncludeMiscLines>N</IncludeMiscLines>",
		"<IncludeCommentLines>N</IncludeCommentLines>",
	} {
		if !strings.Contains(xmlStr, tag) {
			t.Errorf("expected %s in XML, got: %s", tag, xmlStr)
		}
	}
}

func TestBuildSORQRY_RoundTrip(t *testing.T) {
	xmlStr, err := buildSORQRY("SO12345")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	var req sorqryRequest
	if err := xml.Unmarshal([]byte(xmlStr), &req); err != nil {
		t.Fatalf("round-trip unmarshal failed: %v", err)
	}
	if req.Key.SalesOrderNumber != "SO12345" {
		t.Errorf("expected SalesOrderNumber=SO12345, got %q", req.Key.SalesOrderNumber)
	}
	if req.Option.IncludeStockedLines != "N" {
		t.Errorf("expected IncludeStockedLines=N, got %q", req.Option.IncludeStockedLines)
	}
}

const sampleSORQRYResponse = `<SorDetail>
  <Orders>
    <OrderHeader>
      <SalesOrderNumber>SO12345</SalesOrderNumber>
      <OrderStatus>6</OrderStatus>
      <ShippingInstrs>TRACK123</ShippingInstrs>
      <Courier>DPD</Courier>
      <InvoiceDate>2026-03-24</InvoiceDate>
    </OrderHeader>
  </Orders>
</SorDetail>`

func TestParseSORQRY_Success(t *testing.T) {
	result, err := parseSORQRY(sampleSORQRYResponse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SalesOrder != "SO12345" {
		t.Errorf("expected SalesOrder=SO12345, got %q", result.SalesOrder)
	}
	if result.OrderStatus != "6" {
		t.Errorf("expected OrderStatus=6, got %q", result.OrderStatus)
	}
	if result.TrackingNumber != "TRACK123" {
		t.Errorf("expected TrackingNumber=TRACK123, got %q", result.TrackingNumber)
	}
	if result.Carrier != "DPD" {
		t.Errorf("expected Carrier=DPD, got %q", result.Carrier)
	}
	if result.ShippedDate != "2026-03-24" {
		t.Errorf("expected ShippedDate=2026-03-24, got %q", result.ShippedDate)
	}
}

func TestParseSORQRY_Windows1252(t *testing.T) {
	xml1252 := `<?xml version="1.0" encoding="Windows-1252"?>` + sampleSORQRYResponse
	result, err := parseSORQRY(xml1252)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.SalesOrder != "SO12345" {
		t.Errorf("expected SalesOrder=SO12345, got %q", result.SalesOrder)
	}
	if result.OrderStatus != "6" {
		t.Errorf("expected OrderStatus=6, got %q", result.OrderStatus)
	}
}

func TestParseSORQRY_InvalidXML(t *testing.T) {
	_, err := parseSORQRY("<broken>")
	if err == nil {
		t.Fatal("expected error for invalid XML, got nil")
	}
}

func TestQueryDispatchedOrders_Success(t *testing.T) {
	fake := newFakeEnet(t)
	fake.queryResponses["SO12345"] = sampleSORQRYResponse
	fake.queryResponses["SO12346"] = `<SorDetail>
  <Orders>
    <OrderHeader>
      <SalesOrderNumber>SO12346</SalesOrderNumber>
      <OrderStatus>4</OrderStatus>
      <ShippingInstrs></ShippingInstrs>
      <Courier></Courier>
      <InvoiceDate></InvoiceDate>
    </OrderHeader>
  </Orders>
</SorDetail>`
	c := fake.client(t)

	result, err := c.QueryDispatchedOrders(context.Background(), []string{"SO12345", "SO12346"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 2 {
		t.Fatalf("expected 2 results, got %d", len(result))
	}
	if result["SO12345"].OrderStatus != "6" {
		t.Errorf("SO12345: expected OrderStatus=6, got %q", result["SO12345"].OrderStatus)
	}
	if result["SO12345"].TrackingNumber != "TRACK123" {
		t.Errorf("SO12345: expected TrackingNumber=TRACK123, got %q", result["SO12345"].TrackingNumber)
	}
	if result["SO12346"].OrderStatus != "4" {
		t.Errorf("SO12346: expected OrderStatus=4, got %q", result["SO12346"].OrderStatus)
	}
	if fake.logonCalls != 1 {
		t.Errorf("expected 1 logon, got %d", fake.logonCalls)
	}
	if fake.logoffCalls != 1 {
		t.Errorf("expected 1 logoff, got %d", fake.logoffCalls)
	}
	if fake.queryCalls != 2 {
		t.Errorf("expected 2 query calls, got %d", fake.queryCalls)
	}
}

func TestQueryDispatchedOrders_PartialFailure(t *testing.T) {
	fake := newFakeEnet(t)
	fake.queryResponses["SO12345"] = sampleSORQRYResponse
	// SO12346 has no response configured, so fakeEnet returns the default INVQRY-shaped XML
	// which will fail to parse as SORQRY (no OrderHeader).
	c := fake.client(t)

	result, err := c.QueryDispatchedOrders(context.Background(), []string{"SO12345", "SO12346"})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(result) != 1 {
		t.Fatalf("expected 1 result (partial), got %d", len(result))
	}
	if result["SO12345"].OrderStatus != "6" {
		t.Errorf("SO12345: expected OrderStatus=6, got %q", result["SO12345"].OrderStatus)
	}
}

func TestQueryDispatchedOrders_LogonFailure(t *testing.T) {
	fake := newFakeEnet(t)
	fake.logonErr = true
	c := fake.client(t)

	_, err := c.QueryDispatchedOrders(context.Background(), []string{"SO12345"})
	if err == nil {
		t.Fatal("expected error on logon failure, got nil")
	}
	if !strings.Contains(err.Error(), "syspro logon") {
		t.Errorf("error should mention logon, got: %v", err)
	}
}
