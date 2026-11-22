class Mcps < Formula
  desc "One MCP registry for every AI coding agent"
  homepage "https://github.com/chavisnguyen/mcps"
  url "https://github.com/chavisnguyen/mcps/archive/refs/tags/v0.3.0.tar.gz"
  sha256 "32cdaa4cc8095291cc8e367de5d8729207216b63f870da9fbc418daaf11a0760"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}")
  end

  test do
    assert_match "mcps", shell_output("#{bin}/mcps version")
  end
end
