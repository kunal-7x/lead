resource "digitalocean_project" "main" {
  name        = var.project_name
  description = "Lead platform"
  purpose     = "Web Application"
  environment = "Production"
}

resource "digitalocean_vpc" "main" {
  name     = "${var.project_name}-vpc"
  region   = var.region
  ip_range = "10.10.0.0/16"
}

resource "digitalocean_kubernetes_cluster" "doks" {
  name     = "${var.project_name}-doks"
  region   = var.region
  version  = "1.30.1-do.0"
  vpc_uuid = digitalocean_vpc.main.id

  node_pool {
    name       = "default"
    size       = "s-2vcpu-4gb"
    node_count = 2
  }
}

resource "digitalocean_database_cluster" "pg" {
  name       = "${var.project_name}-pg"
  engine     = "pg"
  version    = "16"
  size       = "db-s-1vcpu-1gb"
  region     = var.region
  node_count = 1
  private_network_uuid = digitalocean_vpc.main.id
}

resource "digitalocean_database_cluster" "redis" {
  name       = "${var.project_name}-redis"
  engine     = "redis"
  version    = "7"
  size       = "db-s-1vcpu-1gb"
  region     = var.region
  node_count = 1
  private_network_uuid = digitalocean_vpc.main.id
}

resource "digitalocean_spaces_bucket" "media" {
  name   = "${var.project_name}-media"
  region = var.region
  acl    = "private"
}

resource "digitalocean_container_registry" "docr" {
  name                   = "${var.project_name}-docr"
  subscription_tier_slug = "basic"
  region                 = var.region
}

resource "digitalocean_firewall" "doks" {
  name = "${var.project_name}-doks-fw"
  tags = ["k8s:${digitalocean_kubernetes_cluster.doks.id}"]

  inbound_rule {
    protocol         = "tcp"
    port_range       = "443"
    source_addresses = ["0.0.0.0/0", "::/0"]
  }

  outbound_rule {
    protocol              = "tcp"
    port_range            = "all"
    destination_addresses = ["0.0.0.0/0", "::/0"]
  }
}

resource "b2_bucket" "archive" {
  bucket_name = "${var.project_name}-archive"
  bucket_type = "allPrivate"

  file_lock_configuration {
    mode                  = "compliance"
    default_retention_days = 365
  }
}
