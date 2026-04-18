// Command invbrwtest probes SYSPRO e.net business objects that list stock
// codes in a warehouse. We iterate candidate business object names and XML
// shapes until one returns usable data, then print the raw response so we
// can identify the right root element, record element, and field names for
// the production implementation.
//
// Usage:
//
//	go run ./cmd/invbrwtest [WAREHOUSE]
//
// If WAREHOUSE is omitted, SYSPRO_WAREHOUSE env var is used; falls back to
// empty (no filter).
//
// Findings (fill in after the probe succeeds):
//   Confirmed business object: <TBD>
//   Input XML shape:            <TBD>
//   Response root element:      <TBD>
//   Record element:              <TBD>
//   Stock code field:            <TBD>
//   Pagination needed:           <TBD>
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "FAIL: %v\n", err)
		os.Exit(1)
	}
}

type probe struct {
	name           string // business object name
	description    string
	xmlIn          string
}

func run() error {
	baseURL := requireEnv("SYSPRO_ENET_URL")
	operator := requireEnv("SYSPRO_OPERATOR")
	password := os.Getenv("SYSPRO_PASSWORD")
	companyID := requireEnv("SYSPRO_COMPANY_ID")

	warehouse := ""
	if len(os.Args) >= 2 {
		warehouse = os.Args[1]
	} else {
		warehouse = os.Getenv("SYSPRO_WAREHOUSE")
	}

	baseURL = strings.TrimRight(baseURL, "/")
	client := &http.Client{Timeout: 30 * time.Second}
	ctx := context.Background()

	fmt.Printf("e.net URL:  %s\n", baseURL)
	fmt.Printf("Operator:   %s\n", operator)
	fmt.Printf("Company ID: %s\n", companyID)
	fmt.Printf("Warehouse:  %q (empty = no filter)\n", warehouse)
	fmt.Println()

	fmt.Print("Logon... ")
	guid, err := logon(ctx, client, baseURL, operator, password, companyID)
	if err != nil {
		return fmt.Errorf("logon failed: %w", err)
	}
	fmt.Printf("OK (GUID: %s)\n", guid)

	defer func() {
		fmt.Print("\nLogoff... ")
		if err := logoff(ctx, client, baseURL, guid); err != nil {
			fmt.Printf("WARN: logoff failed: %v\n", err)
		} else {
			fmt.Println("OK")
		}
	}()

	// Candidate business objects ordered by likelihood.
	// Each probe tries one (businessObject, xmlIn) pair.
	whFilter := ""
	if warehouse != "" {
		whFilter = fmt.Sprintf("<Option><WarehouseFilterType>S</WarehouseFilterType><WarehouseFilterValue>%s</WarehouseFilterValue></Option>", warehouse)
	}

	probes := []probe{
		// Sarah says INVQRY should return list-by-warehouse. Try INVQRY with
		// various filter shapes that might enable multi-stock mode.
		{
			name:        "INVQRY",
			description: "INVQRY with StockCodeFilterType=A (all)",
			xmlIn:       fmt.Sprintf("<Query><Key/><Option><StockCodeFilterType>A</StockCodeFilterType><WarehouseFilterType>S</WarehouseFilterType><WarehouseFilterValue>%s</WarehouseFilterValue></Option></Query>", warehouse),
		},
		{
			name:        "INVQRY",
			description: "INVQRY with StockCodeFilterType=R (range) empty bounds",
			xmlIn:       fmt.Sprintf("<Query><Key/><Option><StockCodeFilterType>R</StockCodeFilterType><StockCodeFromFilterValue></StockCodeFromFilterValue><StockCodeToFilterValue>ZZZZZZZZZZZ</StockCodeToFilterValue><WarehouseFilterType>S</WarehouseFilterType><WarehouseFilterValue>%s</WarehouseFilterValue></Option></Query>", warehouse),
		},
		{
			name:        "INVQRY",
			description: "INVQRY with just warehouse option, no Key",
			xmlIn:       fmt.Sprintf("<Query><Option><WarehouseFilterType>S</WarehouseFilterType><WarehouseFilterValue>%s</WarehouseFilterValue></Option></Query>", warehouse),
		},
		{
			name:        "INVQRY",
			description: "INVQRY with no Key no Option",
			xmlIn:       "<Query/>",
		},
		{
			name:        "INVQRX",
			description: "Inventory list query — multi-stock browse",
			xmlIn:       fmt.Sprintf("<Query><Key><StockCode/></Key>%s</Query>", whFilter),
		},
		{
			name:        "INVQWH",
			description: "Inventory warehouse query",
			xmlIn:       fmt.Sprintf("<Query><Key><Warehouse>%s</Warehouse></Key></Query>", warehouse),
		},
		{
			name:        "INVBRW",
			description: "Inventory browse",
			xmlIn:       fmt.Sprintf("<Query><Key><Warehouse>%s</Warehouse></Key></Query>", warehouse),
		},
		{
			name:        "INVBRW",
			description: "Inventory browse — empty key",
			xmlIn:       "<Query><Key/></Query>",
		},
		{
			name:        "INVLST",
			description: "Inventory list",
			xmlIn:       "<Query><Key/></Query>",
		},
		{
			name:        "INVQRY",
			description: "INVQRY with empty stock code",
			xmlIn:       fmt.Sprintf("<Query><Key><StockCode/></Key>%s</Query>", whFilter),
		},
		{
			name:        "COMBGQ",
			description: "Common browse query (table=InvMaster filter=Warehouse)",
			xmlIn:       fmt.Sprintf("<Query><Key><Table>InvMaster</Table></Key><Option><FilterColumn>Warehouse</FilterColumn><FilterValue>%s</FilterValue></Option></Query>", warehouse),
		},
		{
			name:        "INVQWS",
			description: "Inventory warehouse stock query",
			xmlIn:       fmt.Sprintf("<Query><Key><Warehouse>%s</Warehouse></Key></Query>", warehouse),
		},
	}

	for i, p := range probes {
		fmt.Printf("\n=== Probe %d/%d: %s — %s ===\n", i+1, len(probes), p.name, p.description)
		fmt.Printf("XmlIn: %s\n", p.xmlIn)

		params := url.Values{
			"UserId":         {guid},
			"BusinessObject": {p.name},
			"XmlIn":          {p.xmlIn},
		}
		respBody, err := doGet(ctx, client, baseURL+"/Query/Query", params)
		if err != nil {
			fmt.Printf("  RESULT: %v\n", err)
			continue
		}
		// Unwrap JSON-wrapped XML if present.
		var xmlStr string
		if err := json.Unmarshal(respBody, &xmlStr); err != nil {
			xmlStr = string(respBody)
		}
		fmt.Printf("  RESULT: HTTP 200, %d bytes\n", len(xmlStr))
		preview := xmlStr
		if len(preview) > 500 {
			preview = preview[:500] + "... [truncated]"
		}
		fmt.Printf("  Preview: %s\n", preview)

		// Save full response to a per-probe file so we can inspect it later.
		filename := fmt.Sprintf("/tmp/invbrwtest-probe-%d-%s.xml", i+1, p.name)
		if err := os.WriteFile(filename, []byte(xmlStr), 0o600); err == nil {
			fmt.Printf("  Full response saved to %s\n", filename)
		}
	}

	fmt.Println("\n=== Probe complete ===")
	fmt.Println("Review the /tmp/invbrwtest-probe-*.xml files to identify which")
	fmt.Println("business object returned a usable list of stock codes, then update")
	fmt.Println("the comment block at the top of this file with the findings.")

	return nil
}

func logon(ctx context.Context, client *http.Client, baseURL, operator, password, companyID string) (string, error) {
	params := url.Values{
		"Operator":         {operator},
		"OperatorPassword": {password},
		"CompanyId":        {companyID},
	}
	body, err := doGet(ctx, client, baseURL+"/Logon", params)
	if err != nil {
		return "", err
	}
	var guid string
	if err := json.Unmarshal(body, &guid); err != nil {
		guid = strings.TrimSpace(string(body))
	}
	if guid == "" {
		return "", fmt.Errorf("logon returned empty GUID (body: %s)", string(body))
	}
	return guid, nil
}

func logoff(ctx context.Context, client *http.Client, baseURL, guid string) error {
	params := url.Values{"UserId": {guid}}
	_, err := doGet(ctx, client, baseURL+"/Logoff", params)
	return err
}

func doGet(ctx context.Context, client *http.Client, target string, params url.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target+"?"+params.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func requireEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "FAIL: missing required env var %s\n", key)
		os.Exit(1)
	}
	return v
}
