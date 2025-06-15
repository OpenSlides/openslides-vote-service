const std = @import("std");

const Self = @This();

pub const publicKeySize = std.base64.standard_no_pad.Encoder.calcSize(64);

publishKeyPublicFn: *const fn (key: [publicKeySize]u8) CallbackError!void,
logFn: *const fn (message: []const u8) void,

pub fn publishKeyPublic(self: *const Self, key: [publicKeySize]u8) CallbackError!void {
    return self.publishKeyPublicFn(key);
}

pub fn log(self: *const Self, message: []const u8) void {
    self.printFn(message);
}

pub const ErrorCode = enum(i32) {
    Success = 0,
    NetworkError = -1,
    InvalidResponse = -2,
    Timeout = -3,
    Unauthorized = -4,
    NotFound = -5,
    InternalError = -6,
    InvalidInput = -7,
    ProcessingFailed = -8,
    CallbackFailed = -9,

    pub fn fromError(err: anyerror) ErrorCode {
        return switch (err) {
            CallbackError.NetworkError => .NetworkError,
            CallbackError.InvalidResponse => .InvalidResponse,
            CallbackError.Timeout => .Timeout,
            CallbackError.Unauthorized => .Unauthorized,
            CallbackError.NotFound => .NotFound,
            CallbackError.InternalError => .InternalError,
            else => .InternalError,
        };
    }
};

pub const CallbackError = error{
    NetworkError,
    InvalidResponse,
    Timeout,
    Unauthorized,
    NotFound,
    InternalError,
};

pub fn fromErrorCode(code: ErrorCode) CallbackError {
    return switch (code) {
        ErrorCode.NetworkError => error.NetworkError,
        ErrorCode.InvalidResponse => error.InvalidResponse,
        ErrorCode.Timeout => error.Timeout,
        ErrorCode.Unauthorized => error.Unauthorized,
        ErrorCode.NotFound => error.NotFound,
        ErrorCode.InternalError => error.InternalError,
        else => error.InternalError,
    };
}
