import * as pulumi from "@pulumi/pulumi";
/**
 * A pinned k3s release installed and verified on the machine.
 */
export declare class K3sBinary extends pulumi.CustomResource {
    /**
     * Get an existing K3sBinary resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): K3sBinary;
    /**
     * Returns true if the given object is an instance of K3sBinary.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is K3sBinary;
    readonly artifact: pulumi.Output<string | undefined>;
    readonly checksums: pulumi.Output<{
        [key: string]: string;
    } | undefined>;
    readonly path: pulumi.Output<string>;
    readonly version: pulumi.Output<string>;
    readonly written: pulumi.Output<boolean | undefined>;
    /**
     * Create a K3sBinary resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: K3sBinaryArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a K3sBinary resource.
 */
export interface K3sBinaryArgs {
    checksums: pulumi.Input<{
        [key: string]: pulumi.Input<string>;
    }>;
    path?: pulumi.Input<string>;
    version: pulumi.Input<string>;
}
