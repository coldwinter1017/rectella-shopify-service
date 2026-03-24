package syspro

import (
	"context"
	"encoding/xml"
	"fmt"
	"strings"
	"time"
)

// SORQRYResult holds the dispatch-relevant fields from a SYSPRO sales order query.
type SORQRYResult struct {
	SalesOrder     string
	OrderStatus    string // "6" = complete/invoiced
	TrackingNumber string // from ShippingInstrs, may be empty
	Carrier        string // from Courier, may be empty
	ShippedDate    string // from InvoiceDate, may be empty
}

type sorqryRequest struct {
	XMLName xml.Name    `xml:"Query"`
	Key     sorqryKey   `xml:"Key"`
	Option  sorqryOption `xml:"Option"`
}

type sorqryKey struct {
	SalesOrderNumber string `xml:"SalesOrderNumber"`
}

type sorqryOption struct {
	IncludeStockedLines    string `xml:"IncludeStockedLines"`
	IncludeNonStockedLines string `xml:"IncludeNonStockedLines"`
	IncludeFreightLines    string `xml:"IncludeFreightLines"`
	IncludeMiscLines       string `xml:"IncludeMiscLines"`
	IncludeCommentLines    string `xml:"IncludeCommentLines"`
}

type sorqryResponse struct {
	XMLName xml.Name          `xml:"SorDetail"`
	Orders  sorqryOrders      `xml:"Orders"`
}

type sorqryOrders struct {
	OrderHeader *sorqryOrderHeader `xml:"OrderHeader"`
}

type sorqryOrderHeader struct {
	SalesOrderNumber string `xml:"SalesOrderNumber"`
	OrderStatus      string `xml:"OrderStatus"`
	ShippingInstrs   string `xml:"ShippingInstrs"`
	Courier          string `xml:"Courier"`
	InvoiceDate      string `xml:"InvoiceDate"`
}

func buildSORQRY(orderNumber string) (string, error) {
	req := sorqryRequest{
		Key: sorqryKey{SalesOrderNumber: orderNumber},
		Option: sorqryOption{
			IncludeStockedLines:    "N",
			IncludeNonStockedLines: "N",
			IncludeFreightLines:    "N",
			IncludeMiscLines:       "N",
			IncludeCommentLines:    "N",
		},
	}
	b, err := xml.Marshal(req)
	if err != nil {
		return "", fmt.Errorf("marshalling SORQRY request: %w", err)
	}
	return string(b), nil
}

func parseSORQRY(xmlStr string) (*SORQRYResult, error) {
	if i := strings.Index(xmlStr, "?>"); i != -1 {
		xmlStr = strings.TrimSpace(xmlStr[i+2:])
	}
	var resp sorqryResponse
	if err := xml.Unmarshal([]byte(xmlStr), &resp); err != nil {
		return nil, fmt.Errorf("parsing SORQRY response: %w", err)
	}
	if resp.Orders.OrderHeader == nil {
		return nil, fmt.Errorf("parsing SORQRY response: no OrderHeader in response")
	}
	h := resp.Orders.OrderHeader
	return &SORQRYResult{
		SalesOrder:     strings.TrimSpace(h.SalesOrderNumber),
		OrderStatus:    strings.TrimSpace(h.OrderStatus),
		TrackingNumber: strings.TrimSpace(h.ShippingInstrs),
		Carrier:        strings.TrimSpace(h.Courier),
		ShippedDate:    strings.TrimSpace(h.InvoiceDate),
	}, nil
}

// QueryDispatchedOrders queries SYSPRO for the dispatch status of the given
// sales order numbers. Returns a map of order number -> result. Orders that
// fail individually are logged and skipped (partial results returned).
func (c *EnetClient) QueryDispatchedOrders(ctx context.Context, orderNumbers []string) (map[string]SORQRYResult, error) {
	guid, err := c.logon(ctx)
	if err != nil {
		return nil, fmt.Errorf("syspro logon: %w", err)
	}
	defer func() {
		if lerr := c.logoff(ctx, guid); lerr != nil {
			c.logger.Warn("syspro logoff failed", "error", lerr)
		}
	}()

	result := make(map[string]SORQRYResult, len(orderNumbers))
	for _, orderNum := range orderNumbers {
		queryCtx, queryCancel := context.WithTimeout(ctx, 10*time.Second)
		xmlIn, err := buildSORQRY(orderNum)
		if err != nil {
			c.logger.Warn("building SORQRY request", "order", orderNum, "error", err)
			queryCancel()
			continue
		}
		respXML, err := c.query(queryCtx, guid, "SORQRY", xmlIn)
		queryCancel()
		if err != nil {
			c.logger.Warn("SORQRY query failed", "order", orderNum, "error", err)
			continue
		}
		parsed, err := parseSORQRY(respXML)
		if err != nil {
			c.logger.Warn("parsing SORQRY response", "order", orderNum, "error", err)
			continue
		}
		result[orderNum] = *parsed
	}
	return result, nil
}
