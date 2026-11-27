import * as pulumi from "@pulumi/pulumi";
/**
 * Cordon and drain a Kubernetes node when the resource is created or its reinstall token changes.
 */
export declare class CordonAndDrainBarrier extends pulumi.CustomResource {
    /**
     * Get an existing CordonAndDrainBarrier resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): CordonAndDrainBarrier;
    /**
     * Returns true if the given object is an instance of CordonAndDrainBarrier.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is CordonAndDrainBarrier;
    readonly completedAt: pulumi.Output<string | undefined>;
    readonly kubeconfig: pulumi.Output<string | undefined>;
    readonly nodeName: pulumi.Output<string>;
    readonly reinstallToken: pulumi.Output<string | undefined>;
    readonly timeoutSeconds: pulumi.Output<number | undefined>;
    /**
     * Create a CordonAndDrainBarrier resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: CordonAndDrainBarrierArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a CordonAndDrainBarrier resource.
 */
export interface CordonAndDrainBarrierArgs {
    kubeconfig: pulumi.Input<string>;
    nodeName: pulumi.Input<string>;
    reinstallToken: pulumi.Input<string>;
    timeoutSeconds?: pulumi.Input<number>;
}
