/**
 * k3s on a machine you own.
 *
 * Five resources, and deliberately no sixth. There was a native Pulumi k3s provider before this
 * one; its author archived it with the note that it was "way too complicated" and that the command
 * provider could do the job. He was half right. k3s really is a binary, a config file, a unit and a
 * token, and anything that models a cluster as node pools and control-plane topology is describing
 * something k3s does not have. But a command records that it ran and nothing else, so nothing could
 * afterwards say which version is installed or whether the service is even up.
 *
 * So: small, and honest about what the machine says now.
 *
 *   K3sBinary   a pinned release, checksummed, read back from `k3s --version`
 *   K3sServer   the config file and the unit, read back from `systemctl show` and the file
 *   K3sAgent    the same for a worker
 *   NodeToken   the join token a first server generated, for the nodes that come after it
 *   Kubeconfig  the admin credentials, waited for, repointed away from 127.0.0.1, and secret
 *
 * High availability is not a type here. It is `clusterInit` on the first server, `server` and a
 * shared token on the others, and three of them being a control plane that survives losing one.
 */

export {
  K3sBinary,
  readVersion,
  artifactFor,
  parseVersion,
  releaseUrl,
  type K3sBinaryArgs,
} from './resources/binary';

export {
  K3sServer,
  K3sAgent,
  renderConfig,
  renderUnit,
  configFor,
  type K3sServerArgs,
  type K3sAgentArgs,
  type ConfigValue,
} from './resources/node';

export {
  NodeToken,
  readNodeToken,
  nodeTokenPath,
  type NodeTokenArgs,
} from './resources/token';

export {
  Kubeconfig,
  readKubeconfig,
  repoint,
  type KubeconfigArgs,
} from './resources/kubeconfig';
