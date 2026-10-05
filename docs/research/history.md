# Development history

[Documentation index](../README.md)

Selected milestones from the initial client through notification support. Links point to the original repository history; [validation](validation.md) records the tested behavior and its limits.

| Date | Milestone | Reference |
| --- | --- | --- |
| 2026-09-30–10-01 | Built the initial sign-in, DCG enrollment, trust, relay and clipboard stack; merged PR #1. | [`e51728b`](https://github.com/YMGPwcca/phonelink-linux/commit/e51728b12a148061c8076c67200ee4a8f366e423) |
| 2026-10-01 | Added the modular runtime, scoped feature endpoints, live feature control and systemd user service. | [`5ba71ec`](https://github.com/YMGPwcca/phonelink-linux/commit/5ba71ec96c7bafb2f7a1827693da7005e757c221) |
| 2026-10-02 | Added HTML and image clipboard support, MIME polling, image normalization and BMP decoding. | [`ecc54d6`](https://github.com/YMGPwcca/linkmyphone/commit/ecc54d65cc421f192fe9732a092463a526c5a331) · [`2670f26`](https://github.com/YMGPwcca/linkmyphone/commit/2670f2656b9635f5359cd4b2f00945f4a9ab1f0c) |
| 2026-10-03 | Preserved incoming image dimensions, limited PNG size on outbound transfers and moved CI to `main` pushes. | [`7f999f3`](https://github.com/YMGPwcca/linkmyphone/commit/7f999f3b3b5ca7a2857a49e5a6b0ea3b2016f088) · [`98a9e5c`](https://github.com/YMGPwcca/linkmyphone/commit/98a9e5c17a280da3d840b12d61fb50c07fd23931) |
| 2026-10-05 | Added notification sync and one Phone Link sign-in for clipboard and notifications. | [PR #10](https://github.com/YMGPwcca/linkmyphone/pull/10) |

PR #1's description predates later validation and still lists some now-implemented work as pending. Use the later commits and [validation results](validation.md) for current status.
