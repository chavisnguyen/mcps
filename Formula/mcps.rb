class Mcps < Formula
  desc "One MCP registry for every AI coding agent"
  homepage "https://github.com/chavisnguyen/mcps"
  url "https://github.com/chavisnguyen/mcps/archive/refs/tags/v0.5.2.tar.gz"
  sha256 "7dc5653bae59e142af3f25078b29fe7cf296a2abeccfdcc5faa6217ed3e7a585"
  license "MIT"

  depends_on "go" => :build

  def install
    system "go", "build", *std_go_args(ldflags: "-s -w -X main.version=#{version}")
  end

  test do
    assert_match "mcps", shell_output("#{bin}/mcps version")
  end
end
