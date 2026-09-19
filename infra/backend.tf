# G.E.A.R. portable IaC — GCP backend (Story 7.6).
# The backend is parameterized (no hardcoded bucket): set
#   backend "gcs" { bucket = "..."; prefix = "gear" }
# via `tofu init -backend-config` or a `backend.tfvars`. Keeping it parameterized
# means the SAME infra layout can target a different state backend (or, with a
# new provider file, a different cloud) without editing this file.
terraform {
  required_version = ">= 1.5"

  required_providers {
    google = {
      source  = "hashicorp/google"
      version = "~> 5.0"
    }
  }

  # Uncomment + configure for remote state. Leave commented for local state
  # during the trial (the operator chooses).
  # backend "gcs" {}
}