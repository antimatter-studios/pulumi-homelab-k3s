import * as pulumi from "@pulumi/pulumi";
/**
 * The cluster administrator kubeconfig, with the API endpoint repointed.
 */
export declare class Kubeconfig extends pulumi.CustomResource {
    /**
     * Get an existing Kubeconfig resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): Kubeconfig;
    /**
     * Returns true if the given object is an instance of Kubeconfig.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is Kubeconfig;
    readonly config: pulumi.Output<string>;
    readonly readySeconds: pulumi.Output<number | undefined>;
    readonly server: pulumi.Output<string | undefined>;
    /**
     * Create a Kubeconfig resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: KubeconfigArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a Kubeconfig resource.
 */
export interface KubeconfigArgs {
    readySeconds?: pulumi.Input<number>;
    server: pulumi.Input<string>;
}
