package provider

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	_ "unsafe"

	cloudflare "github.com/cloudflare/cloudflare-go"
	"github.com/sst/ion/internal/util"
)

type CloudflareProvider struct {
	client     *cloudflare.API
	identifier *cloudflare.ResourceContainer
	env        map[string]string
	bootstrap  *bootstrap
}

type bootstrap struct {
	State string `json:"state"`
}

func (c *CloudflareProvider) Init(key string, args map[string]interface{}) (err error) {
	accountID := os.Getenv("CLOUDFLARE_DEFAULT_ACCOUNT_ID")
	apiToken := os.Getenv("CLOUDFLARE_API_TOKEN")
	apiKey := os.Getenv("CLOUDFLARE_API_KEY")
	email := os.Getenv("CLOUDFLARE_EMAIL")
	if args["apiToken"] != nil {
		apiToken = args["apiToken"].(string)
	}
	if args["apiKey"] != nil {
		apiKey = args["apiKey"].(string)
	}
	if args["email"] != nil {
		email = args["email"].(string)
	}
	var api *cloudflare.API
	c.env = map[string]string{}
	if apiToken != "" {
		api, _ = cloudflare.NewWithAPIToken(apiToken)
		c.env["CLOUDFLARE_API_TOKEN"] = apiToken
	}
	if apiKey != "" && email != "" {
		api, _ = cloudflare.New(apiKey, email)
		c.env["CLOUDFLARE_API_KEY"] = apiKey
		c.env["CLOUDFLARE_EMAIL"] = email
	}
	if api == nil {
		return util.NewReadableError(nil, "Cloudflare API not initialized. Please provide CLOUDFLARE_API_TOKEN or CLOUDFLARE_API_KEY and CLOUDFLARE_EMAIL environment variables or in the provider section of the project configuration file.")
	}
	c.client = api
	if accountID == "" {
		accounts, _, err := c.client.Accounts(context.Background(), cloudflare.AccountsListParams{})
		if err != nil {
			return err
		}
		accountID = accounts[0].ID
	}
	c.env["CLOUDFLARE_DEFAULT_ACCOUNT_ID"] = accountID
	c.identifier = cloudflare.AccountIdentifier(accountID)
	slog.Info("cloudflare account selected", "account", accountID)
	return nil
}

func (p *CloudflareProvider) Key() string {
	return "cloudflare"
}

func (c *CloudflareProvider) AsHome(app, stage string) (Home, error) {
	ctx := context.Background()
	buckets, err := c.client.ListR2Buckets(ctx, c.identifier, cloudflare.ListR2BucketsParams{
		Name: "sst-state",
	})
	if err != nil {
		return nil, err
	}
	for _, bucket := range buckets {
		if bucket.Name == "sst-state" {
			slog.Info("found existing bucket", "bucket", bucket.Name)
			c.bootstrap = &bootstrap{
				State: bucket.Name,
			}
		}
	}

	if c.bootstrap == nil {
		slog.Info("creating new bucket", "bucket", "sst-state")
		_, err = c.client.CreateR2Bucket(ctx, c.identifier, cloudflare.CreateR2BucketParameters{
			Name: "sst-state",
		})
		if err != nil {
			return nil, err
		}
		c.bootstrap = &bootstrap{
			State: "sst-state",
		}
	}

	return c, nil
}

//go:linkname makeRequestContext github.com/cloudflare/cloudflare-go.(*API).makeRequestContext
func makeRequestContext(*cloudflare.API, context.Context, string, string, interface{}) ([]byte, error)

func (c *CloudflareProvider) putData(kind, app, stage string, data io.Reader) error {
	path := filepath.Join(kind, app, stage)
	_, err := makeRequestContext(c.client, context.Background(), http.MethodPut, "/accounts/"+c.identifier.Identifier+"/r2/buckets/"+c.bootstrap.State+"/objects/"+path, data)
	if err != nil {
		return err
	}
	return nil
}

func (c *CloudflareProvider) getData(kind, app, stage string) (io.Reader, error) {
	path := filepath.Join(kind, app, stage)
	data, err := makeRequestContext(c.client, context.Background(), http.MethodGet, "/accounts/"+c.identifier.Identifier+"/r2/buckets/"+c.bootstrap.State+"/objects/"+path, nil)
	if err != nil {
		if err.Error() == "The specified key does not exist. (10007)" {
			return nil, nil
		}
		return nil, err
	}
	return bytes.NewReader(data), nil
}

func (c *CloudflareProvider) removeData(kind, app, stage string) error {
	path := filepath.Join(kind, app, stage)
	_, err := makeRequestContext(c.client, context.Background(), http.MethodDelete, "/accounts/"+c.identifier.Identifier+"/r2/buckets/"+c.bootstrap.State+"/objects/"+path, nil)
	if err != nil {
		return err
	}
	return nil
}

// these should go into secrets manager once it's out of beta
func (c *CloudflareProvider) setPassphrase(app, stage string, passphrase string) error {
	return c.putData("passphrase", app, stage, bytes.NewReader([]byte(passphrase)))
}

func (c *CloudflareProvider) getPassphrase(app, stage string) (string, error) {
	data, err := c.getData("passphrase", app, stage)
	if err != nil {
		return "", err
	}
	if data == nil {
		return "", nil
	}
	read, err := io.ReadAll(data)
	if err != nil {
		return "", err
	}
	return string(read), nil
}

func (c *CloudflareProvider) Env() (map[string]string, error) {
	return c.env, nil
}
