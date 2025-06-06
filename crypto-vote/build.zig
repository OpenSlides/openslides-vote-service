const std = @import("std");

pub fn build(b: *std.Build) void {
    const target = b.resolveTargetQuery(.{ .cpu_arch = .wasm32, .os_tag = .freestanding });

    const performance = b.addExecutable(.{
        .name = "performance",
        .root_source_file = b.path("src/performance_test.zig"),
        .target = target,
        .optimize = .ReleaseSmall,
    });

    performance.rdynamic = true;
    performance.entry = .disabled;

    b.installArtifact(performance);
}
