package syspro

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"codeberg.org/speeder091/rectella-shopify-service/internal/model"
)

const testGUID = "aaaabbbb-cccc-dddd-eeee-ffffffffffff"

// successfulSORTOIResponse matches the real SYSPRO SORTOI response format.
const successfulSORTOIResponse = `<SalesOrders>
  <Orders>
    <OrderHeader>
      <SalesOrder>SO12345</SalesOrder>
    </OrderHeader>
  </Orders>
  <ValidationStatus>
    <Status>Successful</Status>
  </ValidationStatus>
  <StatusOfItems>
    <ItemsProcessed>000001</ItemsProcessed>
    <ItemsInvalid>000000</ItemsInvalid>
  </StatusOfItems>
</SalesOrders>`

// failedSORTOIResponse simulates a SYSPRO validation failure.
const failedSORTOIResponse = `<SalesOrders>
  <Orders>
    <OrderHeader>
      <SalesOrder/>
    </OrderHeader>
  </Orders>
  <ValidationStatus>
    <Status>Failed</Status>
  </ValidationStatus>
  <StatusOfItems>
    <ItemsProcessed>000001</ItemsProcessed>
    <ItemsInvalid>000001</ItemsInvalid>
  </StatusOfItems>
</SalesOrders>`

// fakeEnet is a configurable httptest server that mimics the SYSPRO e.net REST API.
type fakeEnet struct {
	// Counts of calls per endpoint
	logonCalls    int
	logoffCalls   int
	transactCalls int
	queryCalls    int

	// Behaviour overrides
	logonErr       bool              // return HTTP 500 on /Logon
	transactXML    string            // XML to return from /Transaction (default: successfulSORTOIResponse)
	queryResponses map[string]string // SKU -> response XML for /Query/Query
	queryErr       bool              // return HTTP 500 on /Query/Query

	server *httptest.Server
}

func newFakeEnet(t *testing.T) *fakeEnet {
	t.Helper()
	f := &fakeEnet{transactXML: successfulSORTOIResponse, queryResponses: make(map[string]string)}
	f.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		params := r.URL.Query()

		switch r.URL.Path {
		case "/Logon":
			f.logonCalls++
			if f.logonErr {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(testGUID)

		case "/Logoff":
			f.logoffCalls++
			if params.Get("UserId") != testGUID {
				http.Error(w, "bad UserId", http.StatusBadRequest)
				return
			}
			w.WriteHeader(http.StatusOK)

		case "/Transaction/Post":
			f.transactCalls++
			if params.Get("UserId") != testGUID {
				http.Error(w, "bad UserId", http.StatusBadRequest)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(f.transactXML)

		case "/Query/Query":
			f.queryCalls++
			if f.queryErr {
				http.Error(w, "internal server error", http.StatusInternalServerError)
				return
			}
			if params.Get("UserId") != testGUID {
				http.Error(w, "bad UserId", http.StatusBadRequest)
				return
			}
			xmlIn := params.Get("XmlIn")
			respXML := `<InvQuery><QueryOptions><StockCode>UNKNOWN</StockCode></QueryOptions></InvQuery>`
			for sku, xml := range f.queryResponses {
				if strings.Contains(xmlIn, sku) {
					respXML = xml
					break
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(respXML)

		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(f.server.Close)
	return f
}

func (f *fakeEnet) client(t *testing.T) *EnetClient {
	t.Helper()
	return &EnetClient{
		baseURL:    f.server.URL,
		operator:   "ADMIN",
		password:   "secret",
		companyID:  "RECTELLA",
		logger:     slog.New(slog.NewTextHandler(io.Discard, nil)),
		httpClient: f.server.Client(),
	}
}

func testOrder() model.Order {
	return model.Order{
		OrderNumber:     "#BBQ1001",
		CustomerAccount: "WEBS01",
		OrderDate:       time.Date(2026, 2, 24, 0, 0, 0, 0, time.UTC),
		ShipEmail:       "test@example.com",
		ShipAddress1:    "1 Test Street",
		ShipCity:        "Burnley",
		ShipPostcode:    "BB10 1AA",
		ShipCountry:     "GB",
	}
}

func testLines() []model.OrderLine {
	return []model.OrderLine{
		{SKU: "CBBQ0001", Quantity: 1, UnitPrice: 99.99},
	}
}

// TestSubmitSalesOrder_Success verifies the happy path: logon → transaction → logoff.
func TestSubmitSalesOrder_Success(t *testing.T) {
	fake := newFakeEnet(t)
	c := fake.client(t)

	result, err := c.SubmitSalesOrder(context.Background(), testOrder(), testLines())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if !result.Success {
		t.Errorf("expected Success=true, got false (message: %q)", result.ErrorMessage)
	}
	if result.SysproOrderNumber != "SO12345" {
		t.Errorf("expected SysproOrderNumber=SO12345, got %q", result.SysproOrderNumber)
	}

	if fake.logonCalls != 1 {
		t.Errorf("expected 1 logon call, got %d", fake.logonCalls)
	}
	if fake.transactCalls != 1 {
		t.Errorf("expected 1 transaction call, got %d", fake.transactCalls)
	}
	if fake.logoffCalls != 1 {
		t.Errorf("expected 1 logoff call, got %d", fake.logoffCalls)
	}
}

// TestSubmitSalesOrder_LogonFailure verifies that a logon error is propagated
// and that logoff is NOT attempted (no session to close).
func TestSubmitSalesOrder_LogonFailure(t *testing.T) {
	fake := newFakeEnet(t)
	fake.logonErr = true
	c := fake.client(t)

	_, err := c.SubmitSalesOrder(context.Background(), testOrder(), testLines())
	if err == nil {
		t.Fatal("expected error on logon failure, got nil")
	}
	if !strings.Contains(err.Error(), "syspro logon") {
		t.Errorf("error should mention logon, got: %v", err)
	}

	if fake.logonCalls != 1 {
		t.Errorf("expected 1 logon call, got %d", fake.logonCalls)
	}
	if fake.transactCalls != 0 {
		t.Errorf("expected no transaction calls on logon failure, got %d", fake.transactCalls)
	}
	// Logoff is deferred after a successful logon; if logon fails we return before defer runs.
	if fake.logoffCalls != 0 {
		t.Errorf("expected no logoff call on logon failure, got %d", fake.logoffCalls)
	}
}

// TestSubmitSalesOrder_TransactionSysproError verifies that a SYSPRO business-logic
// error (ReturnCode != 0) is surfaced in SalesOrderResult without returning a Go error.
func TestSubmitSalesOrder_TransactionSysproError(t *testing.T) {
	fake := newFakeEnet(t)
	fake.transactXML = failedSORTOIResponse
	c := fake.client(t)

	result, err := c.SubmitSalesOrder(context.Background(), testOrder(), testLines())
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}

	if result.Success {
		t.Error("expected Success=false for SYSPRO business error")
	}
	if !strings.Contains(result.ErrorMessage, "invalid") {
		t.Errorf("expected error message to mention invalid items, got: %q", result.ErrorMessage)
	}
}

// TestSubmitSalesOrder_LogoffAlwaysCalled verifies that logoff is deferred and called
// even when the transaction itself returns a business-level error.
func TestSubmitSalesOrder_LogoffAlwaysCalled(t *testing.T) {
	fake := newFakeEnet(t)
	fake.transactXML = failedSORTOIResponse
	c := fake.client(t)

	_, _ = c.SubmitSalesOrder(context.Background(), testOrder(), testLines())

	if fake.logoffCalls != 1 {
		t.Errorf("expected logoff to be called even on transaction error, got %d calls", fake.logoffCalls)
	}
}

// TestParseSORTOIResponse_Success exercises the response parser directly.
func TestParseSORTOIResponse_Success(t *testing.T) {
	result, err := parseSORTOIResponse(successfulSORTOIResponse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.SysproOrderNumber != "SO12345" {
		t.Errorf("expected SO12345, got %q", result.SysproOrderNumber)
	}
}

// TestParseSORTOIResponse_Failure exercises the response parser for a failed transaction.
func TestParseSORTOIResponse_Failure(t *testing.T) {
	result, err := parseSORTOIResponse(failedSORTOIResponse)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false")
	}
	if result.ErrorMessage == "" {
		t.Error("expected non-empty error message")
	}
}

// TestParseSORTOIResponse_Windows1252 ensures the parser handles SYSPRO's encoding declaration.
func TestParseSORTOIResponse_Windows1252(t *testing.T) {
	xml1252 := `<?xml version="1.0" encoding="Windows-1252"?>` + successfulSORTOIResponse
	result, err := parseSORTOIResponse(xml1252)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.SysproOrderNumber != "SO12345" {
		t.Errorf("expected SO12345, got %q", result.SysproOrderNumber)
	}
}

// TestParseSORTOIResponse_InvalidXML ensures malformed XML returns an error.
func TestParseSORTOIResponse_InvalidXML(t *testing.T) {
	_, err := parseSORTOIResponse("<broken>")
	if err == nil {
		t.Fatal("expected error for invalid XML, got nil")
	}
}

// ---------------------------------------------------------------------------
// parseSORTOIResponse edge-case tests
// ---------------------------------------------------------------------------

// TestParseSORTOI_EmptySalesOrder documents the real-world SYSPRO RILT behaviour:
// ValidationStatus is "Successful" and ItemsProcessed=000001, but <SalesOrder/>
// is empty (self-closing). The parser should return Success=true and fall back
// to CustomerPoNumber for traceability.
func TestParseSORTOI_EmptySalesOrder(t *testing.T) {
	xml := `<SalesOrders>
  <Orders>
    <OrderHeader>
      <CustomerPoNumber>#TEST-123</CustomerPoNumber>
      <SalesOrder/>
    </OrderHeader>
  </Orders>
  <ValidationStatus>
    <Status>Successful</Status>
  </ValidationStatus>
  <StatusOfItems>
    <ItemsProcessed>000001</ItemsProcessed>
    <ItemsInvalid>000000</ItemsInvalid>
  </StatusOfItems>
</SalesOrders>`

	result, err := parseSORTOIResponse(xml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true for empty SalesOrder with Successful status")
	}
	if result.SysproOrderNumber != "#TEST-123" {
		t.Errorf("expected CustomerPoNumber fallback #TEST-123, got %q", result.SysproOrderNumber)
	}
}

// TestParseSORTOI_WithSalesOrder verifies the normal success path where SYSPRO
// returns an actual sales order number in the response.
func TestParseSORTOI_WithSalesOrder(t *testing.T) {
	xml := `<SalesOrders>
  <Orders>
    <OrderHeader>
      <CustomerPoNumber>#BBQ1001</CustomerPoNumber>
      <SalesOrder>001234</SalesOrder>
    </OrderHeader>
  </Orders>
  <ValidationStatus>
    <Status>Successful</Status>
  </ValidationStatus>
  <StatusOfItems>
    <ItemsProcessed>000001</ItemsProcessed>
    <ItemsInvalid>000000</ItemsInvalid>
  </StatusOfItems>
</SalesOrders>`

	result, err := parseSORTOIResponse(xml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true")
	}
	if result.SysproOrderNumber != "001234" {
		t.Errorf("expected SysproOrderNumber=001234, got %q", result.SysproOrderNumber)
	}
}

// TestParseSORTOI_ValidationFailed verifies that a non-Successful status returns
// Success=false with an error message containing item counts.
func TestParseSORTOI_ValidationFailed(t *testing.T) {
	xml := `<SalesOrders>
  <Orders>
    <OrderHeader>
      <SalesOrder/>
    </OrderHeader>
  </Orders>
  <ValidationStatus>
    <Status>Failed</Status>
  </ValidationStatus>
  <StatusOfItems>
    <ItemsProcessed>000002</ItemsProcessed>
    <ItemsInvalid>000001</ItemsInvalid>
  </StatusOfItems>
</SalesOrders>`

	result, err := parseSORTOIResponse(xml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Success {
		t.Error("expected Success=false for validation failure")
	}
	if !strings.Contains(result.ErrorMessage, "000002") {
		t.Errorf("expected error message to contain ItemsProcessed count, got: %q", result.ErrorMessage)
	}
	if !strings.Contains(result.ErrorMessage, "000001") {
		t.Errorf("expected error message to contain ItemsInvalid count, got: %q", result.ErrorMessage)
	}
}

// TestParseSORTOI_MalformedXML verifies that completely broken XML returns a Go
// error (not a SalesOrderResult with Success=false).
func TestParseSORTOI_MalformedXML(t *testing.T) {
	_, err := parseSORTOIResponse("<SalesOrders><broken")
	if err == nil {
		t.Fatal("expected error for malformed XML, got nil")
	}
	if !strings.Contains(err.Error(), "parsing SORTOI response XML") {
		t.Errorf("expected error to mention parsing, got: %v", err)
	}
}

// TestParseSORTOI_WindowsEncoding verifies that the parser strips the
// encoding="Windows-1252" XML declaration before unmarshalling, since Go's
// xml package does not support that encoding natively.
func TestParseSORTOI_WindowsEncoding(t *testing.T) {
	xml := `<?xml version="1.0" encoding="Windows-1252"?>
<SalesOrders>
  <Orders>
    <OrderHeader>
      <CustomerPoNumber>#BBQ1001</CustomerPoNumber>
      <SalesOrder>005678</SalesOrder>
    </OrderHeader>
  </Orders>
  <ValidationStatus>
    <Status>Successful</Status>
  </ValidationStatus>
  <StatusOfItems>
    <ItemsProcessed>000001</ItemsProcessed>
    <ItemsInvalid>000000</ItemsInvalid>
  </StatusOfItems>
</SalesOrders>`

	result, err := parseSORTOIResponse(xml)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true after stripping Windows-1252 declaration")
	}
	if result.SysproOrderNumber != "005678" {
		t.Errorf("expected SysproOrderNumber=005678, got %q", result.SysproOrderNumber)
	}
}

// TestParseSORTOI_RealLiveResponse uses the actual 2205-byte response captured
// from live SYSPRO testing against company RILT. This documents the exact
// format returned by production SYSPRO, including attributes on the root
// element that the parser must tolerate.
func TestParseSORTOI_RealLiveResponse(t *testing.T) {
	realResponse := `<?xml version="1.0" encoding="Windows-1252"?>
<SalesOrders Language='05' Language2='EN' CssStyle='' DecFormat='1' DateFormat='01' Role='01' Version='8.0.105' OperatorPrimaryRole='017'>
<Orders>
<OrderHeader>
<CustomerPoNumber>#TEST-1774666873</CustomerPoNumber>
<OrderActionType>A</OrderActionType>
<SalesOrder/>
<OrderType>W</OrderType>
</OrderHeader>
</Orders>
<ValidationStatus>
<Status>Successful</Status>
</ValidationStatus>
<StatusOfItems>
<ItemsProcessed>000001</ItemsProcessed>
<ItemsInvalid>000000</ItemsInvalid>
</StatusOfItems>
</SalesOrders>`

	result, err := parseSORTOIResponse(realResponse)
	if err != nil {
		t.Fatalf("unexpected error parsing real live response: %v", err)
	}
	if !result.Success {
		t.Error("expected Success=true for live SYSPRO response")
	}
	// SalesOrder is empty (self-closing), so parser falls back to CustomerPoNumber.
	if result.SysproOrderNumber != "#TEST-1774666873" {
		t.Errorf("expected CustomerPoNumber fallback #TEST-1774666873, got %q", result.SysproOrderNumber)
	}
}

// TestNewEnetClient_Interface verifies the constructor satisfies the Client interface.
func TestNewEnetClient_Interface(t *testing.T) {
	var _ Client = NewEnetClient("http://example.com", "op", "pw", "co", slog.Default())
	// Compile-time check is sufficient; this test documents the guarantee.
	_ = fmt.Sprintf("%T", NewEnetClient)
}
