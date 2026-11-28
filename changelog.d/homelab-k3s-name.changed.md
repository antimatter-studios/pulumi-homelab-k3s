- The provider plugin takes the repository's name: `homelab-k3s` instead of `homelabk3s`. The
  plugin binary is `pulumi-resource-homelab-k3s`, release assets are
  `pulumi-resource-homelab-k3s-v<version>-<os>-<arch>.tar.gz`, resource tokens are
  `homelab-k3s:index:<Type>`, and configuration keys are `homelab-k3s:<key>`. Every resource keeps a
  type alias from `homelabk3s:index:<Type>`, so existing stacks adopt their resources without a
  replace; alias an explicit provider from `pulumi:providers:homelabk3s` in the program as well
  (see the README).
