fn main() {
    let args: Vec<String> = std::env::args().skip(1).collect();
    let result = if args == ["--http-session"] {
        gosvm_svm_runner::serve_http_session()
    } else if args == ["--stdio"] {
        gosvm_svm_runner::serve_stdio()
    } else {
        gosvm_svm_runner::serve_http(args)
    };
    if let Err(error) = result {
        eprintln!("gosvm-svm-runner: {error}");
        std::process::exit(1);
    }
}
