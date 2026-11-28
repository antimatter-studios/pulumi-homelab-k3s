import * as pulumi from "@pulumi/pulumi";
/**
 * Wait for all nodes and embedded etcd members to be healthy and for one node to report the pinned release. Optionally uncordon that node after the checks pass.
 */
export declare class K3sHealthGate extends pulumi.CustomResource {
    /**
     * Get an existing K3sHealthGate resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): K3sHealthGate;
    /**
     * Returns true if the given object is an instance of K3sHealthGate.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is K3sHealthGate;
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
    readonly uncordon: pulumi.Output<boolean | undefined>;
    /**
     * Create a K3sHealthGate resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: K3sHealthGateArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a K3sHealthGate resource.
 */
export interface K3sHealthGateArgs {
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
    uncordon?: pulumi.Input<boolean>;
}
