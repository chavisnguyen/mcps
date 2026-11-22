class Mcps < Formula
  desc "One MCP registry for every AI coding agent"
  homepage "https://github.com/chavisnguyen/mcps"
  url "https://github.com/chavisnguyen/mcps/archive/refs/tags/v0.5.0.tar.gz"
  sha256 "5a7afeb737b2b5be78030579707b352f0578c78cb400cb62c28ea5a411d7193c"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}")
  end

  test do
    assert_match "mcps", shell_output("#{bin}/mcps version")
  end
end
