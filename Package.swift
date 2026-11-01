// swift-tools-version: 6.2
import PackageDescription

let package = Package(
    name: "SockContract",
    platforms: [.iOS(.v26), .macOS(.v26)],
    products: [
        .library(name: "SockContract", targets: ["SockContract"]),
        .library(name: "HistoryTransfer", targets: ["HistoryTransfer"]),
    ],
    dependencies: [
        .package(url: "https://github.com/connectrpc/connect-swift.git", from: "1.2.3"),
        .package(url: "https://github.com/apple/swift-protobuf.git", from: "1.38.1"),
    ],
    targets: [
        .target(
            name: "SockContract",
            dependencies: [
                .product(name: "Connect", package: "connect-swift"),
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "gen/swift"
        ),
        .target(
            name: "HistoryTransfer",
            dependencies: [
                "SockContract",
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "swift/HistoryTransfer"
        ),
        .testTarget(
            name: "HistoryTransferTests",
            dependencies: [
                "HistoryTransfer",
                "SockContract",
                .product(name: "SwiftProtobuf", package: "swift-protobuf"),
            ],
            path: "Tests/HistoryTransferTests"
        ),
        .testTarget(
            name: "SockContractTests",
            dependencies: ["SockContract"],
            path: "Tests/SockContractTests"
        ),
    ]
)
