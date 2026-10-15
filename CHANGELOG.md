# Changelog

## [0.1.2](https://github.com/JakobMelchard/services/compare/v0.1.1...v0.1.2) (2026-10-15)


### Bug Fixes

* **workouts:** require the token on the data reads, HEAD /login is the form ([#47](https://github.com/JakobMelchard/services/issues/47)) ([c9e0134](https://github.com/JakobMelchard/services/commit/c9e0134b6f7a60e103ef0ef6a441fc2c200a1823))
* **workouts:** send the bearer token from the phone app ([#46](https://github.com/JakobMelchard/services/issues/46)) ([ac56315](https://github.com/JakobMelchard/services/commit/ac563152f4d296390855e80682b933fef240a5fb))

## [0.1.1](https://github.com/JakobMelchard/services/compare/v0.1.0...v0.1.1) (2026-10-14)


### Features

* **deploy:** launchd daemons and recipe for lists and workouts ([#4](https://github.com/JakobMelchard/services/issues/4)) ([6964582](https://github.com/JakobMelchard/services/commit/69645827bbe19aaa6fff724d5ce759e8ad981f26))
* **lists:** lists service on the web shell ([#2](https://github.com/JakobMelchard/services/issues/2)) ([9325d38](https://github.com/JakobMelchard/services/commit/9325d38d762b6c3d6277ab172ddd05a94fdc897b))
* **workouts:** Capacitor shells and signed Android release ([#8](https://github.com/JakobMelchard/services/issues/8)) ([2f1683a](https://github.com/JakobMelchard/services/commit/2f1683aee79ea8507c0b98cefbffda7ad8cfdbf2))
* **workouts:** workouts service on the web shell ([#3](https://github.com/JakobMelchard/services/issues/3)) ([a2ca41e](https://github.com/JakobMelchard/services/commit/a2ca41e3b2b6e018a1427ed0c53cf181769d9282))


### Bug Fixes

* **block:** remove a half-written archive tab so a retry keeps the real name ([#41](https://github.com/JakobMelchard/services/issues/41)) ([d29e610](https://github.com/JakobMelchard/services/commit/d29e610d135bc10f4423e4accb84dbbc8bfbee62))
* **deps:** update minor ([#6](https://github.com/JakobMelchard/services/issues/6)) ([96fb9e7](https://github.com/JakobMelchard/services/commit/96fb9e79d6de32df89ac4467795a277fb3b5857e))
* **gsheet:** keep sheet names with only dots inside GSHEET_DIR ([#37](https://github.com/JakobMelchard/services/issues/37)) ([18ed1de](https://github.com/JakobMelchard/services/commit/18ed1de33ba54297eee4a3bb6ad45eec9d9658d4))
* **lists:** refuse cross-site form posts ([#36](https://github.com/JakobMelchard/services/issues/36)) ([a1c883c](https://github.com/JakobMelchard/services/commit/a1c883ca415519896e6644173d1de533243f8d8d))
* **lists:** serialize list delete and copy with item edits ([#22](https://github.com/JakobMelchard/services/issues/22)) ([9e73b6b](https://github.com/JakobMelchard/services/commit/9e73b6b066795e56dff9f0d331dcb6f4e85f9f89))
* **workouts:** keep week rows that have a load but no exercise ([#34](https://github.com/JakobMelchard/services/issues/34)) ([3ecce2b](https://github.com/JakobMelchard/services/commit/3ecce2bd9ded67f00ce6dd13c35bfc8f7145154d))
* **workouts:** never overwrite an archived week when starting a block ([#21](https://github.com/JakobMelchard/services/issues/21)) ([e8446c5](https://github.com/JakobMelchard/services/commit/e8446c5868bfd01f2f9b95f3719992d9067312ee))
* **workouts:** refuse an edit that clears a set's load ([#20](https://github.com/JakobMelchard/services/issues/20)) ([4971c98](https://github.com/JakobMelchard/services/commit/4971c98a9d3bdf4c0fe8653f44de38b80f8763c6))
* **workouts:** sign release APKs with the keystore's own alias ([#19](https://github.com/JakobMelchard/services/issues/19)) ([1d37c52](https://github.com/JakobMelchard/services/commit/1d37c524388db73024ba71aedae718b9fda4b50e))
* **workouts:** stop unescaping path values twice ([#23](https://github.com/JakobMelchard/services/issues/23)) ([3379360](https://github.com/JakobMelchard/services/commit/337936074427de62756255bf2e229ebb5290f9ab))
