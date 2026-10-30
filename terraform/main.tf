terraform {
  required_providers {
    kubernetes = {
      source  = "hashicorp/kubernetes"
      version = "~> 2.37"
    }
  }
}

variable "kubeconfig" {
  type    = string
  default = "../cluster/kubeconfig"
}

provider "kubernetes" {
  config_path = var.kubeconfig
}

resource "kubernetes_namespace" "preview" {
  metadata {
    name = "preview"
    labels = {
      "app.kubernetes.io/managed-by" = "terraform"
    }
  }
}

resource "kubernetes_config_map" "operator" {
  metadata {
    name      = "platform-config"
    namespace = kubernetes_namespace.preview.metadata[0].name
  }
  data = {
    default_replicas = "1"
    controller       = "previewctl"
  }
}

# A PreviewApp instance the controller will reconcile into a
# Deployment + Service once the CRD is applied.
resource "kubernetes_manifest" "demo_app" {
  manifest = {
    apiVersion = "platform.lab/v1alpha1"
    kind       = "PreviewApp"
    metadata = {
      name      = "tf-demo"
      namespace = kubernetes_namespace.preview.metadata[0].name
    }
    spec = {
      image    = "nginx:1.27"
      replicas = 2
      port     = 80
    }
  }
}

output "preview_namespace" {
  value = kubernetes_namespace.preview.metadata[0].name
}
