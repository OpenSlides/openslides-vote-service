const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.resolveTargetQuery(.{ .cpu_arch = .wasm32, .os_tag = .freestanding });

    const wasmCrypto = b.addExecutable(.{
        .name = "crypto",
        .root_source_file = b.path("src/wasm_crypto.zig"),
        .target = target,
        .optimize = .ReleaseSmall,
    });

    wasmCrypto.rdynamic = true;
    wasmCrypto.entry = .disabled;

    const wcf = b.addUpdateSourceFiles();
    wcf.addCopyFileToSource(wasmCrypto.getEmittedBin(), "wrapper/crypto.wasm");

    var update_wasm_crypto_step = b.step("crypto", "Update crypto.wasm");
    update_wasm_crypto_step.dependOn(&wcf.step);

    const wasmApp = b.addExecutable(.{
        .name = "crypto_vote",
        .root_source_file = b.path("src/wasm_app.zig"),
        .target = target,
        .optimize = .ReleaseSmall,
    });

    wasmApp.rdynamic = true;
    wasmApp.entry = .disabled;

    const waf = b.addUpdateSourceFiles();
    waf.addCopyFileToSource(wasmApp.getEmittedBin(), "wrapper/crypto_vote.wasm");

    var update_wasm_app_step = b.step("crypto_vote", "Update crpto_vote.wasm");
    update_wasm_app_step.dependOn(&waf.step);

    const default_step = b.step("default", "Default step");
    default_step.dependOn(update_wasm_crypto_step);
    default_step.dependOn(update_wasm_app_step);

    b.default_step = default_step;
}
