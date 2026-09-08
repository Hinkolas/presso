#!/usr/bin/env ruby
# Structural regression test for the release DAG. GitHub's default job
# condition requires every prerequisite to succeed; do not replace it with
# always() or a condition that accepts skipped jobs.
require 'yaml'
require 'open3'
require 'tmpdir'
release = YAML.load_file(File.join(__dir__, '../.github/workflows/release.yml'))['jobs']
verify = YAML.load_file(File.join(__dir__, '../.github/workflows/verify.yml'))['jobs']
required = %w[verify binary edge runtime]
raise 'publish must require all artifacts and verification' unless (required - release.fetch('publish').fetch('needs')).empty?
%w[binary edge runtime].each do |name|
  raise "#{name} can publish before verification" unless Array(release.fetch(name)['needs']).include?('verify')
end
%w[go install-script web integration].each do |name|
  raise "missing required #{name} verification" unless verify.key?(name)
  raise "#{name} can be conditionally skipped" if verify[name].key?('if') || verify[name]['continue-on-error']
  verify[name].fetch('steps').each do |step|
    raise "#{name} ignores a failed check" if step['continue-on-error']
  end
end
required.concat(['publish']).each do |name|
  job = release.fetch(name)
  raise "#{name} overrides success prerequisites" if job.key?('if') || job['continue-on-error']
end
# Negative cases: any failed, cancelled, or skipped required check must make
# publication ineligible; the passing case must remain eligible.
eligible = ->(states) { release.fetch('publish').fetch('needs').all? { |id| states[id] == 'success' } }
passing = required.to_h { |id| [id, 'success'] }
raise 'passing release is blocked' unless eligible.call(passing)
required.reject { |id| id == 'publish' }.each do |id|
  %w[failure cancelled skipped].each do |state|
    raise "publication accepts #{id}=#{state}" if eligible.call(passing.merge(id => state))
  end
end
puts 'Release gates: passing case and all failure/cancellation/skip cases verified.'

# Exercise the actual publication shell with a recording gh stub: this cannot
# publish anything. v0 tags must be prereleases even without a SemVer suffix.
publish = release.fetch('publish').fetch('steps').find { |step| step['run'] }.fetch('run')
Dir.mktmpdir('presso-release-policy-') do |dir|
  stub = File.join(dir, 'gh')
  File.write(stub, "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \"$PRESSO_TEST_GH_LOG\"\n")
  File.chmod(0755, stub)
  {
    'v0.1.0-rc.1' => '--prerelease --latest=false',
    'v0.1.0' => '--prerelease --latest=false',
    'v0.99.0' => '--prerelease --latest=false',
    'v1.0.0-rc.1' => '--prerelease --latest=false',
    'v1.0.0' => '--latest',
    'v2.1.0' => '--latest'
  }.each do |tag, flags|
    log = File.join(dir, 'calls')
    File.write(log, '')
    env = {
      'PATH' => "#{dir}:#{ENV.fetch('PATH')}",
      'TAG' => tag,
      'GITHUB_REPOSITORY' => 'test/presso',
      'GH_TOKEN' => 'not-a-real-token',
      'PRESSO_TEST_GH_LOG' => log
    }
    out, err, status = Open3.capture3(env, 'bash', '-c', publish)
    raise "publish script failed for #{tag}: #{out}#{err}" unless status.success?
    expected = "release edit #{tag} --repo test/presso --draft=false #{flags}"
    raise "wrong publication flags for #{tag}: #{File.read(log)}" unless File.readlines(log).first&.strip == expected
  end
end
puts 'Release policy: all v0 and suffixed tags are prereleases; v1+ stable tags are latest.'

# Docker builds use separate contexts, so their license/notice copies must
# remain identical to the root originals.
root = File.expand_path('..', __dir__)
%w[LICENSE NOTICE].each do |name|
  %w[build runtime/mu-plugins].each do |dir|
    raise "#{dir}/#{name} differs from root #{name}" unless File.binread(File.join(root, dir, name)) == File.binread(File.join(root, name))
  end
end
puts 'Distribution license and notice copies match.'
