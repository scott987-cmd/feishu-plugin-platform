// Command bitable-bootstrap creates the Base + table that the platform's
// BitableStore expects — using the Feishu OpenAPI directly: NO lark-cli, NO QR
// login. It authenticates with the app's own tenant_access_token (app identity),
// so only FEISHU_APP_ID + FEISHU_APP_SECRET are needed. Feishu is a domestic
// endpoint, so the HTTP client bypasses any HTTPS_PROXY.
//
// Run once, then pass the printed tokens to cmd/api with STORE=bitable:
//
//	FEISHU_APP_ID=... FEISHU_APP_SECRET=... go run ./cmd/bitable-bootstrap
//	# -> prints FEISHU_BITABLE_APP_TOKEN / FEISHU_BITABLE_TABLE_ID
//
// Optional env:
//   - FEISHU_BITABLE_APP_TOKEN: reuse an existing Base instead of creating one.
//   - TABLE_NAME: table name (default "app_definitions").
//
// The app must have Bitable scopes (create app + record read/write). A Base
// created here is owned by the app; delete it from Feishu when done testing.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

const apiBase = "https://open.feishu.cn/open-apis"

// Bitable field type codes: 1 = text, 2 = number.
const (
	fieldText   = 1
	fieldNumber = 2
)

func main() {
	appID := os.Getenv("FEISHU_APP_ID")
	appSecret := os.Getenv("FEISHU_APP_SECRET")
	if appID == "" || appSecret == "" {
		log.Fatal("set FEISHU_APP_ID and FEISHU_APP_SECRET (the app secret is yours to provide)")
	}
	client := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{Proxy: nil}}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	token := tenantToken(ctx, client, appID, appSecret)

	appToken := os.Getenv("FEISHU_BITABLE_APP_TOKEN")
	if appToken == "" {
		appToken = createBase(ctx, client, token, "feishu-plugin-platform store")
		log.Printf("created Base: app_token=%s", appToken)
	} else {
		log.Printf("reusing Base: app_token=%s", appToken)
	}

	tableName := os.Getenv("TABLE_NAME")
	if tableName == "" {
		tableName = "app_definitions"
	}
	tableID := createTable(ctx, client, token, appToken, tableName)
	log.Printf("created table %q: table_id=%s", tableName, tableID)

	fmt.Println()
	fmt.Println("# paste these into the cmd/api environment (STORE=bitable):")
	fmt.Println("FEISHU_BITABLE_APP_TOKEN=" + appToken)
	fmt.Println("FEISHU_BITABLE_TABLE_ID=" + tableID)
}

func tenantToken(ctx context.Context, c *http.Client, appID, appSecret string) string {
	var out struct {
		Code  int    `json:"code"`
		Msg   string `json:"msg"`
		Token string `json:"tenant_access_token"`
	}
	post(ctx, c, "", apiBase+"/auth/v3/tenant_access_token/internal",
		map[string]string{"app_id": appID, "app_secret": appSecret}, &out)
	if out.Code != 0 {
		log.Fatalf("tenant_access_token: code %d: %s", out.Code, out.Msg)
	}
	return out.Token
}

func createBase(ctx context.Context, c *http.Client, token, name string) string {
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			App struct {
				AppToken string `json:"app_token"`
			} `json:"app"`
		} `json:"data"`
	}
	post(ctx, c, token, apiBase+"/bitable/v1/apps", map[string]any{"name": name}, &out)
	if out.Code != 0 {
		log.Fatalf("create base: code %d: %s", out.Code, out.Msg)
	}
	return out.Data.App.AppToken
}

func createTable(ctx context.Context, c *http.Client, token, appToken, name string) string {
	body := map[string]any{"table": map[string]any{
		"name": name,
		"fields": []map[string]any{
			{"field_name": "id", "type": fieldText},
			{"field_name": "name", "type": fieldText},
			{"field_name": "version", "type": fieldNumber},
			{"field_name": "definition", "type": fieldText},
		},
	}}
	var out struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			TableID string `json:"table_id"`
		} `json:"data"`
	}
	post(ctx, c, token, apiBase+"/bitable/v1/apps/"+appToken+"/tables", body, &out)
	if out.Code != 0 {
		log.Fatalf("create table: code %d: %s", out.Code, out.Msg)
	}
	return out.Data.TableID
}

// post does a JSON POST (with optional bearer token) and decodes into out.
func post(ctx context.Context, c *http.Client, token, url string, body, out any) {
	raw, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		log.Fatal(err)
	}
	req.Header.Set("content-type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := c.Do(req)
	if err != nil {
		log.Fatalf("request %s: %v", url, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode/100 != 2 {
		log.Fatalf("%s: http %d: %s", url, resp.StatusCode, string(data))
	}
	if err := json.Unmarshal(data, out); err != nil {
		log.Fatalf("decode %s: %v", url, err)
	}
}
