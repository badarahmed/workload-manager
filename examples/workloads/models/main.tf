terraform {
  required_providers {
    wm = { source = "homelab/wm" }
  }
}

module "d3" {
  source = "./d3"
}

output "workloads" {
  value = { d3 = module.d3.status }
}
