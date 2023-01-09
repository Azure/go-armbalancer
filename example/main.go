package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore/policy"
	"github.com/Azure/azure-sdk-for-go/sdk/azidentity"

	"github.com/Azure/armbalancer"
)

func main() {
	cred, err := azidentity.NewAzureCLICredential(nil)
	if err != nil {
		panic(err)
	}

	token, err := cred.GetToken(context.TODO(), policy.TokenRequestOptions{Scopes: []string{"https://management.core.windows.net"}})
	if err != nil {
		panic(err)
	}

	client := &http.Client{Transport: armbalancer.New(http.DefaultTransport.(*http.Transport), "management.azure.com", 4, 11997, 0)}

	for {
		do(client, token)
		time.Sleep(time.Millisecond * 50)
	}
}

func do(client *http.Client, token azcore.AccessToken) {
	req, err := http.NewRequest("GET", os.Getenv("TEST_RESOURCE"), nil)
	if err != nil {
		panic(err)
	}
	req.Header.Set("Authorization", "Bearer "+token.Token)

	resp, err := client.Do(req)
	if err != nil {
		panic(err)
	}
	defer resp.Body.Close()

	log.Printf("response status %d with rate limit remaining %s", resp.StatusCode, resp.Header.Get("x-ms-ratelimit-remaining-subscription-reads"))
}
