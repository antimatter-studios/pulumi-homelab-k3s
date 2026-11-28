import * as pulumi from "@pulumi/pulumi";
/**
 * Render cloud-config user data for a k3s server or agent on a systemd image. Use the Output form to retain the secrecy of the user data.
 */
export declare function renderCloudInit(args: RenderCloudInitArgs, opts?: pulumi.InvokeOptions): Promise<RenderCloudInitResult>;
export interface RenderCloudInitArgs {
    binary?: string;
    /**
     * SHA-256 checksums keyed by release artifact, as in K3sBinary.
     */
    checksums: {
        [key: string]: string;
    };
    clusterInit?: boolean;
    dataDir?: string;
    disable?: string[];
    enabled?: boolean;
    extra?: {
        [key: string]: any;
    };
    kubeletArg?: string[];
    nodeLabel?: string[];
    nodeName?: string;
    nodeTaint?: string[];
    requiresMount?: string;
    /**
     * The k3s role: server or agent. Use the corresponding resource inputs for that role.
     */
    role: string;
    server?: string;
    snapshotter?: string;
    started?: boolean;
    tlsSan?: string[];
    token?: string;
    /**
     * The pinned k3s release tag, as in K3sBinary.
     */
    version: string;
}
export interface RenderCloudInitResult {
    readonly userData: string;
}
/**
 * Render cloud-config user data for a k3s server or agent on a systemd image. Use the Output form to retain the secrecy of the user data.
 */
export declare function renderCloudInitOutput(args: RenderCloudInitOutputArgs, opts?: pulumi.InvokeOutputOptions): pulumi.Output<RenderCloudInitResult>;
export interface RenderCloudInitOutputArgs {
    binary?: pulumi.Input<string>;
    /**
     * SHA-256 checksums keyed by release artifact, as in K3sBinary.
     */
    checksums: pulumi.Input<{
        [key: string]: pulumi.Input<string>;
    }>;
    clusterInit?: pulumi.Input<boolean>;
    dataDir?: pulumi.Input<string>;
    disable?: pulumi.Input<pulumi.Input<string>[]>;
    enabled?: pulumi.Input<boolean>;
    extra?: pulumi.Input<{
        [key: string]: any;
    }>;
    kubeletArg?: pulumi.Input<pulumi.Input<string>[]>;
    nodeLabel?: pulumi.Input<pulumi.Input<string>[]>;
    nodeName?: pulumi.Input<string>;
    nodeTaint?: pulumi.Input<pulumi.Input<string>[]>;
    requiresMount?: pulumi.Input<string>;
    /**
     * The k3s role: server or agent. Use the corresponding resource inputs for that role.
     */
    role: pulumi.Input<string>;
    server?: pulumi.Input<string>;
    snapshotter?: pulumi.Input<string>;
    started?: pulumi.Input<boolean>;
    tlsSan?: pulumi.Input<pulumi.Input<string>[]>;
    token?: pulumi.Input<string>;
    /**
     * The pinned k3s release tag, as in K3sBinary.
     */
    version: pulumi.Input<string>;
}
