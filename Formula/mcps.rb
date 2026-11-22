class Mcps < Formula
  desc "One MCP registry for every AI coding agent"
  homepage "https://github.com/chavisnguyen/mcps"
  url "https://github.com/chavisnguyen/mcps/archive/refs/tags/v0.5.1.tar.gz"
  sha256 "c3a0913084deef3c967ff1e897aafe811945497fcbf677ce86169ebca82b5d6a"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}")
  end

  test do
    assert_match "mcps", shell_output("#{bin}/mcps version")
  end
end
