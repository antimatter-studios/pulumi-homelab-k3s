import * as pulumi from "@pulumi/pulumi";
/**
 * Read a Contabo instance and resolve an optional image name.
 */
export declare class ContaboInstanceValidation extends pulumi.CustomResource {
    /**
     * Get an existing ContaboInstanceValidation resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): ContaboInstanceValidation;
    /**
     * Returns true if the given object is an instance of ContaboInstanceValidation.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is ContaboInstanceValidation;
    readonly clientId: pulumi.Output<string | undefined>;
    readonly clientSecret: pulumi.Output<string | undefined>;
    readonly fingerprint: pulumi.Output<{
        [key: string]: any;
    } | undefined>;
    readonly instanceId: pulumi.Output<string>;
    readonly osImage: pulumi.Output<string | undefined>;
    readonly pass: pulumi.Output<string | undefined>;
    readonly raw: pulumi.Output<any | undefined>;
    readonly resolvedImage: pulumi.Output<any | undefined>;
    readonly user: pulumi.Output<string | undefined>;
    /**
     * Create a ContaboInstanceValidation resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: ContaboInstanceValidationArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a ContaboInstanceValidation resource.
 */
export interface ContaboInstanceValidationArgs {
    clientId: pulumi.Input<string>;
    clientSecret: pulumi.Input<string>;
    instanceId: pulumi.Input<string>;
    osImage?: pulumi.Input<string>;
    pass: pulumi.Input<string>;
    user: pulumi.Input<string>;
}
