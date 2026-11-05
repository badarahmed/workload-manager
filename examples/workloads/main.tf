module "apps" {
  source = "./apps"
}

module "models" {
  source = "./models"
}

output "apps" {
  value = module.apps.workloads
}

output "models" {
  value = module.models.workloads
}
