const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.resolveTargetQuery(.{ .cpu_arch = .wasm32, .os_tag = .freestanding });

    const wasmCrypto = b.addExecutable(.{
        .name = "crypto",
        .root_module = b.createModule(.{
            .root_source_file = b.path("src/wasm_crypto.zig"),
            .optimize = .ReleaseSmall,
            .target = target,
        }),
    });

    wasmCrypto.rdynamic = true;
    wasmCrypto.entry = .disabled;

    const wcf = b.addUpdateSourceFiles();
    wcf.addCopyFileToSource(wasmCrypto.getEmittedBin(), "wrapper/crypto.wasm");

    var update_wasm_crypto_step = b.step("crypto", "Update crypto.wasm");
    update_wasm_crypto_step.dependOn(&wcf.step);

    const default_step = b.step("default", "Default step");
    default_step.dependOn(update_wasm_crypto_step);

    b.default_step = default_step;
}
