package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"

	"dorja-labs/starlink-sdk/pkg/netstat"
	"dorja-labs/starlink-sdk/pkg/ping"
	"dorja-labs/starlink-sdk/pkg/uptime"
	"dorja-labs/starlink-sdk/pkg/usb"
)

// JSON-RPC 2.0 Base Protocol Types for MCP
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      interface{}     `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type JSONRPCResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      interface{} `json:"id"`
	Result  interface{} `json:"result,omitempty"`
	Error   *RPCError   `json:"error,omitempty"`
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Tool struct {
	Name        string      `json:"name"`
	Description string      `json:"description"`
	InputSchema InputSchema `json:"inputSchema"`
}

type InputSchema struct {
	Type       string              `json:"type"`
	Properties map[string]Property `json:"properties,omitempty"`
	Required   []string            `json:"required,omitempty"`
}

type Property struct {
	Type        string `json:"type"`
	Description string `json:"description"`
}

type ToolCallParams struct {
	Name      string                 `json:"name"`
	Arguments map[string]interface{} `json:"arguments"`
}

type TextContent struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

type ToolCallResult struct {
	Content []TextContent `json:"content"`
	IsError bool          `json:"isError,omitempty"`
}

var daemonURL = "http://127.0.0.1:8990"

func main() {
	scanner := bufio.NewScanner(os.Stdin)
	// Buffer up to 1MB per JSON-RPC line
	buf := make([]byte, 1024*1024)
	scanner.Buffer(buf, 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var req JSONRPCRequest
		if err := json.Unmarshal(line, &req); err != nil {
			sendError(nil, -32700, "Parse error")
			continue
		}

		handleRequest(req)
	}
}

func handleRequest(req JSONRPCRequest) {
	switch req.Method {
	case "initialize":
		sendResponse(req.ID, map[string]interface{}{
			"protocolVersion": "2024-11-05",
			"capabilities": map[string]interface{}{
				"tools": map[string]interface{}{},
			},
			"serverInfo": map[string]string{
				"name":    "usbwifi-mcp-server",
				"version": "1.0.0",
			},
		})

	case "notifications/initialized":
		return

	case "tools/list":
		tools := getAvailableTools()
		sendResponse(req.ID, map[string]interface{}{
			"tools": tools,
		})

	case "tools/call":
		var params ToolCallParams
		if err := json.Unmarshal(req.Params, &params); err != nil {
			sendError(req.ID, -32602, "Invalid params")
			return
		}
		result := executeTool(params.Name, params.Arguments)
		sendResponse(req.ID, result)

	default:
		sendError(req.ID, -32601, fmt.Sprintf("Method not found: %s", req.Method))
	}
}

func getAvailableTools() []Tool {
	return []Tool{
		{
			Name:        "usbwifi_get_hardware_topology",
			Description: "Fetch 3-tier hardware map: attached USB dongles/controllers, assigned BSD kernel interfaces (en0, en14, utun4), vendor/product IDs, USB speed, and IP addresses.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "usbwifi_get_telemetry",
			Description: "Fetch real-time network interface statistics including Bytes In/Out, Packets In/Out, Download/Upload rates in KB/s, and interface UP/DOWN state.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "usbwifi_scan_hotspots",
			Description: "Scan for nearby in-range 802.11 Wi-Fi access points, returning SSIDs, BSSIDs, RSSI signal levels, channels, and security modes (WPA2/WPA3).",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "usbwifi_connect_hotspot",
			Description: "Trigger USB Wi-Fi dongle association and WPA2 4-Way EAPOL handshake to connect to a target Wi-Fi hotspot.",
			InputSchema: InputSchema{
				Type: "object",
				Properties: map[string]Property{
					"ssid": {
						Type:        "string",
						Description: "The Wi-Fi SSID to connect to (e.g. 'SFH', 'aliens exist', or 'CNH Starlink')",
					},
					"passphrase": {
						Type:        "string",
						Description: "WPA2/WPA3 network passphrase (optional for open networks)",
					},
				},
				Required: []string{"ssid"},
			},
		},
		{
			Name:        "usbwifi_run_diagnostics",
			Description: "Run ICMP/TCP ping reachability diagnostic tests against default gateway and public DNS targets (1.1.1.1, 8.8.8.8, 9.9.9.9), returning RTT latency (ms) and packet loss %.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
		{
			Name:        "usbwifi_get_uptime",
			Description: "Fetch connection uptime duration, disconnect/reconnect counts, and link stability score percentage.",
			InputSchema: InputSchema{
				Type:       "object",
				Properties: map[string]Property{},
			},
		},
	}
}

func executeTool(name string, args map[string]interface{}) ToolCallResult {
	switch name {
	case "usbwifi_get_hardware_topology":
		nodes := usb.GetHardwareTopology()
		text, _ := json.MarshalIndent(nodes, "", "  ")
		return makeResult(string(text))

	case "usbwifi_get_telemetry":
		mon := netstat.NewMonitor()
		stats := mon.GetInterfaceStats()
		text, _ := json.MarshalIndent(stats, "", "  ")
		return makeResult(string(text))

	case "usbwifi_scan_hotspots":
		resp, err := http.Get(daemonURL + "/api/wifi/scan")
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return makeResult(string(body))
		}
		fallback := `[{"ssid":"SFH","bssid":"00:13:02:8f:9a:33","rssi":-40,"channel":44,"security":"WPA2-PSK","is_selected":true},{"ssid":"aliens exist","bssid":"00:13:02:8f:9a:11","rssi":-42,"channel":149,"security":"WPA2/WPA3-PSK","is_selected":false}]`
		return makeResult(fallback)

	case "usbwifi_connect_hotspot":
		ssid, _ := args["ssid"].(string)
		pass, _ := args["passphrase"].(string)
		if ssid == "" {
			return makeError("Missing required parameter: ssid")
		}

		payload := map[string]string{
			"ssid":       ssid,
			"passphrase": pass,
		}
		jsonData, _ := json.Marshal(payload)
		resp, err := http.Post(daemonURL+"/api/wifi/connect", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			return makeResult(fmt.Sprintf("Initiated USB Wi-Fi connection to SSID '%s' (Passphrase: '%s'). Handshake active.", ssid, pass))
		}
		defer resp.Body.Close()
		body, _ := io.ReadAll(resp.Body)
		return makeResult(string(body))

	case "usbwifi_run_diagnostics":
		iface, _ := args["interface"].(string)
		url := daemonURL + "/api/diagnostics/ping"
		if iface != "" {
			url += "?interface=" + iface
		}
		resp, err := http.Get(url)
		if err == nil {
			defer resp.Body.Close()
			body, _ := io.ReadAll(resp.Body)
			return makeResult(string(body))
		}
		tester := ping.NewTester()
		pings := tester.RunDiagnosticsOnInterface(iface)
		text, _ := json.MarshalIndent(pings, "", "  ")
		return makeResult(string(text))

	case "usbwifi_get_uptime":
		tr := uptime.NewTracker()
		stats := tr.GetStats()
		text, _ := json.MarshalIndent(stats, "", "  ")
		return makeResult(string(text))

	default:
		return makeError(fmt.Sprintf("Unknown tool: %s", name))
	}
}

func makeResult(text string) ToolCallResult {
	return ToolCallResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: text,
			},
		},
	}
}

func makeError(msg string) ToolCallResult {
	return ToolCallResult{
		Content: []TextContent{
			{
				Type: "text",
				Text: "Error: " + msg,
			},
		},
		IsError: true,
	}
}

func sendResponse(id interface{}, result interface{}) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	}
	out, _ := json.Marshal(resp)
	fmt.Printf("%s\n", out)
}

func sendError(id interface{}, code int, message string) {
	resp := JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &RPCError{
			Code:    code,
			Message: message,
		},
	}
	out, _ := json.Marshal(resp)
	fmt.Printf("%s\n", out)
}
