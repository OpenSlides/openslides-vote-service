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

    const wf = b.addUpdateSourceFiles();
    wf.addCopyFileToSource(wasm.getEmittedBin(), "wrapper/crypto_vote.wasm");

    const update_wasm_step = b.step("wasm", "Update crypto_vote.wasm");
    update_wasm_step.dependOn(&wf.step);

    b.default_step = update_wasm_step;

    //b.installArtifact(wasm);
}
