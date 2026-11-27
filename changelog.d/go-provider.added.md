- Ported `K3sServer`, `K3sAgent`, `K3sBinary`, `NodeToken`, and `Kubeconfig` to the Go provider
  with live reads and import support. The generated Node.js SDK is `@antimatter-studios/homelab-k3s`
  and connects using the pulumi-homelab SSH transport under the `homelabk3s` configuration namespace.
