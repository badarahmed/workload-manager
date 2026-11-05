terraform {
  required_providers {
    wm = { source = "homelab/wm" }
  }
}

variable "wm_root" {
  type        = string
  description = "Absolute wm checkout path, set by the tofu launcher."
}

provider "wm" {
  root = abspath(pathexpand(var.wm_root))
}
