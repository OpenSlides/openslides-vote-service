const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.resolveTargetQuery(.{ .cpu_arch = .wasm32, .os_tag = .freestanding });

    const wasm = b.addExecutable(.{
        .name = "crypto_vote",
        .root_source_file = b.path("src/wasm.zig"),
        .target = target,
        .optimize = .ReleaseSmall,
    });

    wasm.rdynamic = true;
    wasm.entry = .disabled;

    b.installArtifact(wasm);
}
