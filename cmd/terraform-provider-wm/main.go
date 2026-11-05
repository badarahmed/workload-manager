package main

import (
	"context"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"homelab/wm/internal/provider"
	"log"
)

func main() {
	if err := providerserver.Serve(context.Background(), provider.New, providerserver.ServeOpts{Address: "registry.opentofu.org/homelab/wm"}); err != nil {
		log.Fatal(err)
	}
}
