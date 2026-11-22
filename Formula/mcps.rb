class Mcps < Formula
  desc "One MCP registry for every AI coding agent"
  homepage "https://github.com/chavisnguyen/mcps"
  url "https://github.com/chavisnguyen/mcps/archive/refs/tags/v0.3.1.tar.gz"
  sha256 "89fb77158419875d184ef13c6e983d5c42d83997fd60bcb2de658756ca36dc25"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}")
  end

  test do
    assert_match "mcps", shell_output("#{bin}/mcps version")
  end
end
