output "doks_endpoint" {
  value     = digitalocean_kubernetes_cluster.doks.endpoint
  sensitive = true
}

output "docr_endpoint" {
  value = digitalocean_container_registry.docr.endpoint
}

output "pg_uri" {
  value     = digitalocean_database_cluster.pg.uri
  sensitive = true
}

output "redis_uri" {
  value     = digitalocean_database_cluster.redis.uri
  sensitive = true
}
