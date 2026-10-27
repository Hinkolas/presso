# Every image the integration tests run, built in one parallel bake:
#
#   WP_VERSION=7.1 docker buildx bake --load
#
# tags presso-{edge,mariadb,valkey,browser,runtime}:release-verification.
# verify.yml sets PHP_VERSION, PHP_PATCH and WP_VERSION from
# `pressod runtime-matrix` and CACHE=gha to reuse layers between CI runs.
# Releases do not use this file: release.yml and runtime.yml build each
# image per architecture.

variable "TAG" {
  default = "release-verification"
}

variable "CACHE" {
  default = ""
}

variable "PHP_VERSION" {
  default = "8.5"
}

variable "PHP_PATCH" {
  default = "8.5.12"
}

variable "WP_VERSION" {
  default = ""
}

# Scopes are separate from the release workflows' caches.
function "cache_from" {
  params = [name]
  result = CACHE == "gha" ? ["type=gha,scope=verify-${name}"] : []
}

function "cache_to" {
  params = [name]
  result = CACHE == "gha" ? ["type=gha,scope=verify-${name},mode=max"] : []
}

group "default" {
  targets = ["platform", "runtime"]
}

target "platform" {
  name       = image
  matrix     = { image = ["edge", "mariadb", "valkey", "browser"] }
  context    = "build"
  dockerfile = "${image}.Dockerfile"
  tags       = ["presso-${image}:${TAG}"]
  cache-from = cache_from(image)
  cache-to   = cache_to(image)
}

target "runtime-base" {
  context    = "runtime"
  dockerfile = "base.Dockerfile"
  args = {
    PHP_VERSION = PHP_VERSION
    PHP_PATCH   = PHP_PATCH
  }
  tags       = ["presso-runtime-base:${TAG}"]
  cache-from = cache_from("runtime-base-php${PHP_VERSION}")
  cache-to   = cache_to("runtime-base-php${PHP_VERSION}")
}

target "runtime" {
  context    = "runtime"
  dockerfile = "Dockerfile"
  contexts = {
    base = "target:runtime-base"
  }
  args = {
    BASE_IMAGE = "base"
    WP_VERSION = WP_VERSION
  }
  tags       = ["presso-runtime:${TAG}"]
  cache-from = cache_from("runtime")
  cache-to   = cache_to("runtime")
}
