terraform {
  required_version = ">= 1.6.0"

  required_providers {
    digitalocean = {
      source  = "digitalocean/digitalocean"
      version = "~> 2.40"
    }
    b2 = {
      source  = "Backblaze/b2"
      version = "~> 0.9"
    }
  }
}
