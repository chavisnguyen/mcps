class Mcps < Formula
  desc "One MCP registry for every AI coding agent"
  homepage "https://github.com/chavisnguyen/mcps"
  license "MIT"

  # NOTE: `url`/`sha256` below are rewritten on every published Release by
  # the "Release assets" workflow (.github/workflows/release.yml). Do not edit
  # by hand. (`version` is inferred from the tag in the URL.)
  livecheck do
    url :homepage
    strategy :github_latest
  end

  on_macos do
    on_arm do
      url "https://github.com/chavisnguyen/mcps/releases/download/v0.5.3/mcps-darwin-arm64.tar.gz"
      sha256 "8a737a6120ae0331ef4329117b7d09b6f7e0293918ff9737370aeb9296b223fd"
    end
    on_intel do
      url "https://github.com/chavisnguyen/mcps/releases/download/v0.5.3/mcps-darwin-amd64.tar.gz"
      sha256 "750be9e39bda855526a461e9604a9d3de8a75faa49d0b8a7cd9f8b5ac392f835"
    end
  end

  on_linux do
    on_arm do
      url "https://github.com/chavisnguyen/mcps/releases/download/v0.5.3/mcps-linux-arm64.tar.gz"
      sha256 "e5f211b00f0dfd3953733a76a10b3e88e6c37090a3559b34ad2e5798cac0951c"
    end
    on_intel do
      url "https://github.com/chavisnguyen/mcps/releases/download/v0.5.3/mcps-linux-amd64.tar.gz"
      sha256 "dcc88bc9b4b9dda94e5efcc03e9d51410232c44df9adf51d7d455754351489c3"
    end
  end

  def install
    bin.install "mcps"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/mcps version")
  end
end
