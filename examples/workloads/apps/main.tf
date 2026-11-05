terraform {
  required_providers {
    wm = { source = "homelab/wm" }
  }
}

module "web" {
  source = "./web"
}

output "workloads" {
  value = { web = module.web.status }
}
