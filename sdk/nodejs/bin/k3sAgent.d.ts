import * as pulumi from "@pulumi/pulumi";
/**
 * A k3s worker and its systemd unit.
 */
export declare class K3sAgent extends pulumi.CustomResource {
    /**
     * Get an existing K3sAgent resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): K3sAgent;
    /**
     * Returns true if the given object is an instance of K3sAgent.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is K3sAgent;
    readonly config: pulumi.Output<string>;
    readonly configMode: pulumi.Output<string>;
    readonly enabled: pulumi.Output<boolean>;
    readonly started: pulumi.Output<boolean>;
    readonly unit: pulumi.Output<string>;
    readonly unitMode: pulumi.Output<string>;
    readonly written: pulumi.Output<boolean | undefined>;
    /**
     * Create a K3sAgent resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: K3sAgentArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a K3sAgent resource.
 */
export interface K3sAgentArgs {
    binary?: pulumi.Input<string>;
    dataDir?: pulumi.Input<string>;
    enabled?: pulumi.Input<boolean>;
    extra?: pulumi.Input<{
        [key: string]: any;
    }>;
    kubeletArg?: pulumi.Input<pulumi.Input<string>[]>;
    nodeLabel?: pulumi.Input<pulumi.Input<string>[]>;
    nodeName?: pulumi.Input<string>;
    nodeTaint?: pulumi.Input<pulumi.Input<string>[]>;
    requiresMount?: pulumi.Input<string>;
    server: pulumi.Input<string>;
    snapshotter?: pulumi.Input<string>;
    started?: pulumi.Input<boolean>;
    token: pulumi.Input<string>;
}
