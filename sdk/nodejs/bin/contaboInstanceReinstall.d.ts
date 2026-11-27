import * as pulumi from "@pulumi/pulumi";
/**
 * Reinstall a Contabo instance when the resource is created or its reinstall token changes.
 */
export declare class ContaboInstanceReinstall extends pulumi.CustomResource {
    /**
     * Get an existing ContaboInstanceReinstall resource's state with the given name, ID, and optional extra
     * properties used to qualify the lookup.
     *
     * @param name The _unique_ name of the resulting resource.
     * @param id The _unique_ provider ID of the resource to lookup.
     * @param opts Optional settings to control the behavior of the CustomResource.
     */
    static get(name: string, id: pulumi.Input<pulumi.ID>, opts?: pulumi.CustomResourceOptions): ContaboInstanceReinstall;
    /**
     * Returns true if the given object is an instance of ContaboInstanceReinstall.  This is designed to work even
     * when multiple copies of the Pulumi SDK have been loaded into the same process.
     */
    static isInstance(obj: any): obj is ContaboInstanceReinstall;
    readonly clientId: pulumi.Output<string | undefined>;
    readonly clientSecret: pulumi.Output<string | undefined>;
    readonly defaultUser: pulumi.Output<string | undefined>;
    readonly drainBeforeReinstall: pulumi.Output<boolean | undefined>;
    readonly drainTimeoutSeconds: pulumi.Output<number | undefined>;
    readonly imageId: pulumi.Output<string | undefined>;
    readonly instanceId: pulumi.Output<string>;
    readonly kubeconfig: pulumi.Output<string | undefined>;
    readonly nodeName: pulumi.Output<string | undefined>;
    readonly osImage: pulumi.Output<string | undefined>;
    readonly pass: pulumi.Output<string | undefined>;
    readonly reinstallToken: pulumi.Output<string>;
    readonly reinstalledAt: pulumi.Output<string | undefined>;
    readonly sshKeySecretId: pulumi.Output<number | undefined>;
    readonly sshPublicKey: pulumi.Output<string | undefined>;
    readonly user: pulumi.Output<string | undefined>;
    readonly userData: pulumi.Output<string | undefined>;
    /**
     * Create a ContaboInstanceReinstall resource with the given unique name, arguments, and options.
     *
     * @param name The _unique_ name of the resource.
     * @param args The arguments to use to populate this resource's properties.
     * @param opts A bag of options that control this resource's behavior.
     */
    constructor(name: string, args: ContaboInstanceReinstallArgs, opts?: pulumi.CustomResourceOptions);
}
/**
 * The set of arguments for constructing a ContaboInstanceReinstall resource.
 */
export interface ContaboInstanceReinstallArgs {
    clientId: pulumi.Input<string>;
    clientSecret: pulumi.Input<string>;
    defaultUser?: pulumi.Input<string>;
    drainBeforeReinstall?: pulumi.Input<boolean>;
    drainTimeoutSeconds?: pulumi.Input<number>;
    instanceId: pulumi.Input<string>;
    kubeconfig?: pulumi.Input<string>;
    nodeName?: pulumi.Input<string>;
    osImage: pulumi.Input<string>;
    pass: pulumi.Input<string>;
    reinstallToken: pulumi.Input<string>;
    sshPublicKey: pulumi.Input<string>;
    user: pulumi.Input<string>;
    userData?: pulumi.Input<string>;
}
