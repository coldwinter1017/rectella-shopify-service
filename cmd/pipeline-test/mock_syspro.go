package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

type mockSyspro struct {
	port     int
	orderSeq atomic.Int64
	server   *http.Server

	// Track submitted orders so SORQRY can return status "9" for them.
	mu              sync.Mutex
	submittedOrders map[string]string // syspro order number -> "submitted"
}

func newMockSyspro(port int) *mockSyspro {
	m := &mockSyspro{
		port:            port,
		submittedOrders: make(map[string]string),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/SYSPROWCFService/Rest/Logon", m.handleLogon)
	mux.HandleFunc("/SYSPROWCFService/Rest/Transaction/Post", m.handleTransaction)
	mux.HandleFunc("/SYSPROWCFService/Rest/Query/Query", m.handleQuery)
	mux.HandleFunc("/SYSPROWCFService/Rest/Logoff", m.handleLogoff)
	m.server = &http.Server{
		Addr:              fmt.Sprintf(":%d", port),
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}
	return m
}

func (m *mockSyspro) start() error {
	go func() { _ = m.server.ListenAndServe() }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", fmt.Sprintf("localhost:%d", m.port), 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("mock SYSPRO did not start on port %d within 2s", m.port)
}

func (m *mockSyspro) stop() { _ = m.server.Close() }

func (m *mockSyspro) handleLogon(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprint(w, "mock-session-001")
}

func (m *mockSyspro) handleTransaction(w http.ResponseWriter, r *http.Request) {
	seq := m.orderSeq.Add(1)
	orderNum := fmt.Sprintf("SO-MOCK-%03d", seq)

	// Track submitted order for SORQRY.
	m.mu.Lock()
	m.submittedOrders[orderNum] = "submitted"
	m.mu.Unlock()

	// Read and discard body to avoid connection issues.
	_, _ = io.ReadAll(r.Body)

	w.Header().Set("Content-Type", "text/xml")
	const sortioResp = "<?xml version=\"1.0\" encoding=\"Windows-1252\"?>\n<SalesOrders>\n  <Orders><OrderHeader>\n    <SalesOrder>%s</SalesOrder>\n    <CustomerPoNumber></CustomerPoNumber>\n  </OrderHeader></Orders>\n  <ValidationStatus><Status>Successful</Status></ValidationStatus>\n  <StatusOfItems><ItemsProcessed>1</ItemsProcessed><ItemsInvalid>0</ItemsInvalid></StatusOfItems>\n</SalesOrders>"
	_, _ = fmt.Fprintf(w, sortioResp, orderNum) //nolint:gosec // nosemgrep
}

func (m *mockSyspro) handleQuery(w http.ResponseWriter, r *http.Request) {
	bo := r.URL.Query().Get("BusinessObject")
	w.Header().Set("Content-Type", "text/xml")

	switch bo {
	case "SORQRY":
		// Extract order number from XmlIn.
		xmlIn := r.URL.Query().Get("XmlIn")
		orderNum := extractXMLValue(xmlIn, "SalesOrder")

		m.mu.Lock()
		_, found := m.submittedOrders[orderNum]
		m.mu.Unlock()

		const sorqryComplete = "<?xml version=\"1.0\" encoding=\"Windows-1252\"?>\n<SorDetail>\n  <SalesOrder>%s</SalesOrder>\n  <OrderStatus>9</OrderStatus>\n  <OrderStatusDesc>Complete</OrderStatusDesc>\n  <ShippingInstrs>MockCarrier</ShippingInstrs>\n  <ShippingInstrsCod>MCR</ShippingInstrsCod>\n  <LastInvoice>MOCK-INV-001</LastInvoice>\n</SorDetail>"
		const sorqryOpen = "<?xml version=\"1.0\" encoding=\"Windows-1252\"?>\n<SorDetail>\n  <SalesOrder>%s</SalesOrder>\n  <OrderStatus>1</OrderStatus>\n  <OrderStatusDesc>Open</OrderStatusDesc>\n  <ShippingInstrs></ShippingInstrs>\n</SorDetail>"
		if found {
			_, _ = fmt.Fprintf(w, sorqryComplete, orderNum) //nolint:gosec // nosemgrep
		} else {
			_, _ = fmt.Fprintf(w, sorqryOpen, orderNum) //nolint:gosec // nosemgrep
		}

	default:
		// INVQRY — extract stock code from XmlIn to return per-SKU data.
		xmlIn := r.URL.Query().Get("XmlIn")
		sku := extractXMLValue(xmlIn, "StockCode")
		if sku == "" {
			sku = "UNKNOWN"
		}
		const invqryResp = "<?xml version=\"1.0\" encoding=\"Windows-1252\"?>\n<InvQuery>\n  <QueryOptions>\n    <StockCode>%s</StockCode>\n    <Description>Mock stock item</Description>\n  </QueryOptions>\n  <WarehouseItem>\n    <Warehouse>WH01</Warehouse>\n    <QtyOnHand>150.000</QtyOnHand>\n    <AvailableQty>100.000</AvailableQty>\n  </WarehouseItem>\n</InvQuery>"
		_, _ = fmt.Fprintf(w, invqryResp, sku) //nolint:gosec // nosemgrep
	}
}

func (m *mockSyspro) handleLogoff(w http.ResponseWriter, _ *http.Request) {
	_, _ = fmt.Fprint(w, "true")
}

// extractXMLValue is a quick-and-dirty XML value extractor for mock use.
func extractXMLValue(xml, tag string) string {
	open := "<" + tag + ">"
	close := "</" + tag + ">"
	start := strings.Index(xml, open)
	if start == -1 {
		return ""
	}
	start += len(open)
	end := strings.Index(xml[start:], close)
	if end == -1 {
		return ""
	}
	return strings.TrimSpace(xml[start : start+end])
}
