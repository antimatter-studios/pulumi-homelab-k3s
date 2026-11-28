import * as pulumi from "@pulumi/pulumi";
/**
 * Wait for cluster health, then cordon and drain one node using the eviction API. The provider connects to an existing embedded-etcd server.
 */
export declare class K3sDrain extends pulumi.CustomResource {
    /**
     * Get an existing K3sDrain resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): K3sDrain;
    /**
     * Returns true if the given object is an instance of K3sDrain.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is K3sDrain;
    readonly approvedRelease: pulumi.Output<string>;
    readonly completedAt: pulumi.Output<string>;
    /**
     * k3s data directory on the provider host. Defaults to /var/lib/rancher/k3s.
     */
    readonly etcdDataDir: pulumi.Output<string | undefined>;
    /**
     * Expected number of embedded etcd voters. At least three are required.
     */
    readonly etcdMembers: pulumi.Output<number>;
    /**
     * k3s executable on the provider host. Defaults to /usr/local/bin/k3s.
     */
    readonly k3sPath: pulumi.Output<string | undefined>;
    /**
     * Kubeconfig on the provider host. Defaults to /etc/rancher/k3s/k3s.yaml.
     */
    readonly kubeconfigPath: pulumi.Output<string | undefined>;
    /**
     * Name of the Kubernetes node to maintain.
     */
    readonly nodeName: pulumi.Output<string>;
    /**
     * Pinned release that triggers maintenance. The post-upgrade gate waits for this exact kubelet version.
     */
    readonly release: pulumi.Output<string>;
    /**
     * Deadline in seconds for each health wait and maintenance command. Defaults to 300.
     */
    readonly timeoutSeconds: pulumi.Output<number | undefined>;
    /**
     * Create a K3sDrain resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: K3sDrainArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a K3sDrain resource.
 */
export interface K3sDrainArgs {
    /**
     * k3s data directory on the provider host. Defaults to /var/lib/rancher/k3s.
     */
    etcdDataDir?: pulumi.Input<string>;
    /**
     * Expected number of embedded etcd voters. At least three are required.
     */
    etcdMembers: pulumi.Input<number>;
    /**
     * k3s executable on the provider host. Defaults to /usr/local/bin/k3s.
     */
    k3sPath?: pulumi.Input<string>;
    /**
     * Kubeconfig on the provider host. Defaults to /etc/rancher/k3s/k3s.yaml.
     */
    kubeconfigPath?: pulumi.Input<string>;
    /**
     * Name of the Kubernetes node to maintain.
     */
    nodeName: pulumi.Input<string>;
    /**
     * Pinned release that triggers maintenance. The post-upgrade gate waits for this exact kubelet version.
     */
    release: pulumi.Input<string>;
    /**
     * Deadline in seconds for each health wait and maintenance command. Defaults to 300.
     */
    timeoutSeconds?: pulumi.Input<number>;
}
